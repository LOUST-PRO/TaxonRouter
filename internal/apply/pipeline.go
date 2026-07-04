// Package apply implements the GitHub write-back pipeline.
package apply

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// Config for the apply pipeline.
type Config struct {
	ProjectNumber     int
	PollInterval     time.Duration
	DryRun           bool
	Logger           *slog.Logger
	FieldMapping     map[string]string
	FieldValueMapping map[string]string
}

// Pipeline orchestrates the apply worker.
type Pipeline struct {
	cfg    Config
	gh     GitHubWriter
	logger *slog.Logger
	queue  chan Item
	done   chan struct{}
	wg     sync.WaitGroup
}

// Item is one PR pending application to GitHub.
type Item struct {
	Repo          Repo
	Labels        []string
	ProjectFields map[string]string
	Confidence    float64
	EnqueuedAt    time.Time
}

// Repo identifies a GitHub repository and PR number.
type Repo struct {
	Owner  string
	Repo   string
	Number int
}

// GitHubWriter is the interface for GitHub API writes.
type GitHubWriter interface {
	CurrentLabels(ctx context.Context, owner, repo string, number int) (map[string]struct{}, error)
	AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error
	AddToProject(ctx context.Context, owner, repo string, number int, projectID string) (string, error)
	SetProjectFields(ctx context.Context, projectID, itemID string, fields map[string]string) error
	CurrentProjectFields(ctx context.Context, projectID, itemID string, fieldNames []string) (map[string]string, error)
	GetProjectID(ctx context.Context, owner, repo string, projectNumber int) (string, error)
}

// New returns a Pipeline ready to be started.
func New(cfg Config, gh GitHubWriter) *Pipeline {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 10 * time.Second
	}
	return &Pipeline{
		cfg:    cfg,
		gh:     gh,
		logger: logger,
		queue:  make(chan Item, 1024),
		done:   make(chan struct{}),
	}
}

// Submit enqueues a PR for GitHub write-back.
func (p *Pipeline) Submit(ctx context.Context, owner, repo string, number int, labels []string, fields map[string]string) error {
	item := Item{
		Repo:          Repo{Owner: owner, Repo: repo, Number: number},
		Labels:        labels,
		ProjectFields: fields,
		EnqueuedAt:    time.Now(),
	}
	select {
	case p.queue <- item:
		return nil
	default:
		p.logger.Warn("apply queue full; dropping item",
			slog.String("repo", owner+"/"+repo), slog.Int("pr", number))
		return context.DeadlineExceeded
	}
}

// Start runs the apply worker in a background goroutine.
func (p *Pipeline) Start() {
	if p.gh == nil {
		p.logger.Info("apply pipeline: no GitHub client; pipeline disabled")
		return
	}
	p.logger.Info("apply pipeline: starting",
		slog.Bool("dry_run", p.cfg.DryRun), slog.Int("project_number", p.cfg.ProjectNumber))
	p.wg.Add(1)
	go p.worker()
}

// Stop signals the worker to shut down cleanly.
func (p *Pipeline) Stop() {
	close(p.done)
	p.wg.Wait()
	p.logger.Info("apply pipeline: stopped")
}

func (p *Pipeline) worker() {
	defer p.wg.Done()
	for {
		select {
		case <-p.done:
			for {
				select {
				case item := <-p.queue:
					p.applyItem(context.Background(), item)
				default:
					return
				}
			}
		case item := <-p.queue:
			p.applyItem(context.Background(), item)
		}
	}
}

func (p *Pipeline) applyItem(ctx context.Context, item Item) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	owner, repo := item.Repo.Owner, item.Repo.Repo
	number := item.Repo.Number

	existingLabels, err := p.gh.CurrentLabels(ctx, owner, repo, number)
	if err != nil {
		p.logger.Warn("apply: current labels read failed",
			slog.String("repo", owner+"/"+repo), slog.Int("pr", number), slog.String("err", err.Error()))
	} else {
		var toAdd []string
		for _, l := range item.Labels {
			if _, already := existingLabels[l]; !already {
				toAdd = append(toAdd, l)
			}
		}
		if len(toAdd) > 0 {
			if p.cfg.DryRun {
				p.logger.Info("apply [DRY RUN]: would add labels",
					slog.String("repo", owner+"/"+repo), slog.Int("pr", number), slog.Any("labels", toAdd))
			} else {
				if err := p.gh.AddLabels(ctx, owner, repo, number, toAdd); err != nil {
					p.logger.Error("apply: add labels failed",
						slog.String("repo", owner+"/"+repo), slog.Int("pr", number), slog.String("err", err.Error()))
				} else {
					p.logger.Info("apply: labels added",
						slog.String("repo", owner+"/"+repo), slog.Int("pr", number), slog.Any("labels", toAdd))
				}
			}
		}
	}

	if p.cfg.ProjectNumber <= 0 || len(item.ProjectFields) == 0 {
		return
	}
	projectID, err := p.gh.GetProjectID(ctx, owner, repo, p.cfg.ProjectNumber)
	if err != nil {
		p.logger.Warn("apply: get project id failed",
			slog.String("repo", owner+"/"+repo), slog.Int("project", p.cfg.ProjectNumber), slog.String("err", err.Error()))
		return
	}
	itemID, err := p.gh.AddToProject(ctx, owner, repo, number, projectID)
	if err != nil {
		p.logger.Error("apply: add to project failed",
			slog.String("repo", owner+"/"+repo), slog.Int("pr", number), slog.String("err", err.Error()))
		return
	}
	if itemID == "" {
		p.logger.Debug("apply: pr already in project, skipping field set",
			slog.String("repo", owner+"/"+repo), slog.Int("pr", number))
		return
	}

	currentFields, err := p.gh.CurrentProjectFields(ctx, projectID, itemID, sortedKeys(item.ProjectFields))
	if err != nil {
		p.logger.Warn("apply: current fields read failed",
			slog.String("repo", owner+"/"+repo), slog.Int("pr", number), slog.String("err", err.Error()))
		currentFields = map[string]string{}
	}

	toSet := make(map[string]string)
	for k, v := range item.ProjectFields {
		fieldName := k
		if p.cfg.FieldMapping != nil {
			if mapped, ok := p.cfg.FieldMapping[k]; ok {
				fieldName = mapped
			}
		}
		value := v
		if p.cfg.FieldValueMapping != nil {
			if mapped, ok := p.cfg.FieldValueMapping[fieldName+":"+v]; ok {
				value = mapped
			}
		}
		if currentFields[fieldName] == value {
			p.logger.Debug("apply: field already set, skipping",
				slog.String("repo", owner+"/"+repo), slog.Int("pr", number),
				slog.String("field", fieldName), slog.String("value", value))
		} else {
			toSet[fieldName] = value
		}
	}
	if len(toSet) == 0 {
		return
	}
	if p.cfg.DryRun {
		p.logger.Info("apply [DRY RUN]: would set project fields",
			slog.String("repo", owner+"/"+repo), slog.Int("pr", number), slog.Any("fields", toSet))
		return
	}
	if err := p.gh.SetProjectFields(ctx, projectID, itemID, toSet); err != nil {
		p.logger.Error("apply: set project fields failed",
			slog.String("repo", owner+"/"+repo), slog.Int("pr", number), slog.String("err", err.Error()))
	} else {
		p.logger.Info("apply: project fields set",
			slog.String("repo", owner+"/"+repo), slog.Int("pr", number), slog.Any("fields", toSet))
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
