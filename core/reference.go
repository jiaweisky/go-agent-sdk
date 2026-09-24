package core

import (
	"sync"

	"github.com/user/go-agent-sdk/schema"
)

type (
	ReferenceLocalSource = schema.ReferenceLocalSource
	ReferenceGitSource   = schema.ReferenceGitSource
	ReferenceSource      = schema.ReferenceSource
	ReferenceInfo        = schema.ReferenceInfo
)

// ReferenceEntry is a named reference source.
type ReferenceEntry struct {
	Name   string
	Source schema.ReferenceSource
}

type referenceData struct {
	sources map[string]schema.ReferenceSource
}

// ReferenceDraft is the mutable view passed to registered transforms.
type ReferenceDraft struct {
	Add    func(name string, source schema.ReferenceSource)
	Remove func(name string)
	List   func() []ReferenceEntry
}

// Reference mirrors Reference.Service.
type Reference struct {
	state *State[*referenceData, ReferenceDraft]

	mu           sync.Mutex
	materialized map[string]schema.ReferenceInfo
}

// NewReference builds the reference service. events and cache may be nil.
func NewReference(global Global, events Event, cache *RepositoryCache, batch ...*StateBatch) *Reference {
	reference := &Reference{materialized: map[string]schema.ReferenceInfo{}}
	options := StateOptions[*referenceData, ReferenceDraft]{
		Initial: func() *referenceData {
			return &referenceData{sources: map[string]schema.ReferenceSource{}}
		},
		Draft: func(data *referenceData) ReferenceDraft {
			return ReferenceDraft{
				Add:    func(name string, source schema.ReferenceSource) { data.sources[name] = source },
				Remove: func(name string) { delete(data.sources, name) },
				List: func() []ReferenceEntry {
					entries := make([]ReferenceEntry, 0, len(data.sources))
					for name, source := range data.sources {
						entries = append(entries, ReferenceEntry{Name: name, Source: source})
					}
					return entries
				},
			}
		},
		Finalize: func(draft ReferenceDraft) error {
			reference.materialize(global, cache, draft)
			if events != nil {
				_, _ = events.Publish(schema.ReferenceEventUpdated, map[string]any{}, nil)
			}
			return nil
		},
	}
	if len(batch) > 0 {
		options.Batch = batch[0]
	}
	reference.state = NewState(options)
	return reference
}

func (r *Reference) materialize(global Global, cache *RepositoryCache, draft ReferenceDraft) {
	materialized := map[string]schema.ReferenceInfo{}
	for _, entry := range draft.List() {
		switch source := entry.Source.(type) {
		case schema.ReferenceLocalSource:
			materialized[entry.Name] = schema.ReferenceInfo{
				Name: entry.Name, Path: source.Path, Description: source.Description, Hidden: source.Hidden, Source: source,
			}
		case schema.ReferenceGitSource:
			repository := RepositoryParse(source.Repository, cache.repositoryOptions()...)
			if repository == nil || !RepositoryIsRemote(repository) {
				continue
			}
			if source.Branch != nil {
				if err := RepositoryValidateBranch(*source.Branch); err != nil {
					continue
				}
			}
			path := schema.AbsolutePath(RepositoryCachePath(global.Repos, repository, source.Branch))
			materialized[entry.Name] = schema.ReferenceInfo{
				Name: entry.Name, Path: path, Description: source.Description, Hidden: source.Hidden, Source: source,
			}
			if cache != nil {
				reference, branch := repository, source.Branch
				go func() {
					_, _ = cache.Ensure(RepositoryCacheEnsureInput{Reference: reference, Refresh: true, Branch: branch})
				}()
			}
		}
	}
	r.mu.Lock()
	r.materialized = materialized
	r.mu.Unlock()
}

func (r *Reference) Transform(update func(ReferenceDraft) error) (func() error, error) {
	return r.state.Transform(update)
}

func (r *Reference) Reload() error { return r.state.Reload() }

// List returns the materialized reference infos.
func (r *Reference) List() []schema.ReferenceInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]schema.ReferenceInfo, 0, len(r.materialized))
	for _, info := range r.materialized {
		result = append(result, info)
	}
	return result
}
