package cache

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"time"

	"github.com/MichaelMure/git-bug/bug"
	"github.com/MichaelMure/git-bug/entity"
	"github.com/MichaelMure/git-bug/query"
	"github.com/MichaelMure/git-bug/repository"
)

const bugCacheFile = "bug-cache"

var errBugNotInCache = errors.New("bug missing from cache")

func bugCacheFilePath(repo repository.Repo) string {
	return path.Join(repo.GetPath(), "git-bug", bugCacheFile)
}

// bugUpdated is a callback to trigger when the excerpt of a bug changed,
// that is each time a bug is updated
func (c *RepoCache) bugUpdated(id entity.Id) error {
	c.muBug.Lock()
	b, ok := c.bugs[id]
	if !ok {
		c.muBug.Unlock()

		// if the bug is not loaded at this point, it means it was loaded before
		// but got evicted. Which means we potentially have multiple copies in
		// memory and thus concurrent write.
		// Failing immediately here is the simple and safe solution to avoid
		// complicated data loss.
		return errBugNotInCache
	}
	c.loadedBugs.Get(id)
	c.bugExcerpts[id] = NewBugExcerpt(b.bug, b.Snapshot())
	c.muBug.Unlock()

	// we only need to write the bug cache
	return c.writeBugCache()
}

// load will try to read from the disk the bug cache file
func (c *RepoCache) loadBugCache() error {
	c.muBug.Lock()
	defer c.muBug.Unlock()

	f, err := os.Open(bugCacheFilePath(c.repo))
	if err != nil {
		return err
	}

	decoder := gob.NewDecoder(f)

	aux := struct {
		Version  uint
		Excerpts map[entity.Id]*BugExcerpt
	}{}

	err = decoder.Decode(&aux)
	if err != nil {
		return err
	}

	if aux.Version != formatVersion {
		return fmt.Errorf("unknown cache format version %v", aux.Version)
	}

	c.bugExcerpts = aux.Excerpts
	return nil
}

// write will serialize on disk the bug cache file
func (c *RepoCache) writeBugCache() error {
	c.muBug.RLock()
	defer c.muBug.RUnlock()

	var data bytes.Buffer

	aux := struct {
		Version  uint
		Excerpts map[entity.Id]*BugExcerpt
	}{
		Version:  formatVersion,
		Excerpts: c.bugExcerpts,
	}

	encoder := gob.NewEncoder(&data)

	err := encoder.Encode(aux)
	if err != nil {
		return err
	}

	f, err := os.Create(bugCacheFilePath(c.repo))
	if err != nil {
		return err
	}

	_, err = f.Write(data.Bytes())
	if err != nil {
		return err
	}

	return f.Close()
}

// ResolveBugExcerpt retrieve a BugExcerpt matching the exact given id
func (c *RepoCache) ResolveBugExcerpt(id entity.Id) (*BugExcerpt, error) {
	c.muBug.RLock()
	defer c.muBug.RUnlock()

	excerpt, ok := c.bugExcerpts[id]
	if !ok {
		return nil, bug.ErrBugNotExist
	}

	return excerpt, nil
}

// ResolveBug retrieve a bug matching the exact given id
func (c *RepoCache) ResolveBug(id entity.Id) (*BugCache, error) {
	c.muBug.RLock()
	cached, ok := c.bugs[id]
	if ok {
		c.loadedBugs.Get(id)
		c.muBug.RUnlock()
		return cached, nil
	}
	c.muBug.RUnlock()

	b, err := bug.ReadLocalWithResolver(c.repo, newIdentityCacheResolver(c), id)
	if err != nil {
		return nil, err
	}

	cached = NewBugCache(c, b)

	c.muBug.Lock()
	c.bugs[id] = cached
	c.loadedBugs.Add(id)
	c.muBug.Unlock()

	c.evictIfNeeded()

	return cached, nil
}

// evictIfNeeded will evict a bug from the cache if needed
// it also removes references of the bug from the bugs
func (c *RepoCache) evictIfNeeded() {
	c.muBug.Lock()
	defer c.muBug.Unlock()
	if c.loadedBugs.Len() <= c.maxLoadedBugs {
		return
	}

	for _, id := range c.loadedBugs.GetOldestToNewest() {
		b := c.bugs[id]
		if b.NeedCommit() {
			continue
		}

		b.mu.Lock()
		c.loadedBugs.Remove(id)
		delete(c.bugs, id)

		if c.loadedBugs.Len() <= c.maxLoadedBugs {
			return
		}
	}
}

// ResolveBugExcerptPrefix retrieve a BugExcerpt matching an id prefix. It fails if multiple
// bugs match.
func (c *RepoCache) ResolveBugExcerptPrefix(prefix string) (*BugExcerpt, error) {
	return c.ResolveBugExcerptMatcher(func(excerpt *BugExcerpt) bool {
		return excerpt.Id.HasPrefix(prefix)
	})
}

// ResolveBugPrefix retrieve a bug matching an id prefix. It fails if multiple
// bugs match.
func (c *RepoCache) ResolveBugPrefix(prefix string) (*BugCache, error) {
	return c.ResolveBugMatcher(func(excerpt *BugExcerpt) bool {
		return excerpt.Id.HasPrefix(prefix)
	})
}

// ResolveBugCreateMetadata retrieve a bug that has the exact given metadata on
// its Create operation, that is, the first operation. It fails if multiple bugs
// match.
func (c *RepoCache) ResolveBugCreateMetadata(key string, value string) (*BugCache, error) {
	return c.ResolveBugMatcher(func(excerpt *BugExcerpt) bool {
		return excerpt.CreateMetadata[key] == value
	})
}

func (c *RepoCache) ResolveBugExcerptMatcher(f func(*BugExcerpt) bool) (*BugExcerpt, error) {
	id, err := c.resolveBugMatcher(f)
	if err != nil {
		return nil, err
	}
	return c.ResolveBugExcerpt(id)
}

func (c *RepoCache) ResolveBugMatcher(f func(*BugExcerpt) bool) (*BugCache, error) {
	id, err := c.resolveBugMatcher(f)
	if err != nil {
		return nil, err
	}
	return c.ResolveBug(id)
}

func (c *RepoCache) resolveBugMatcher(f func(*BugExcerpt) bool) (entity.Id, error) {
	c.muBug.RLock()
	defer c.muBug.RUnlock()

	// preallocate but empty
	matching := make([]entity.Id, 0, 5)

	for _, excerpt := range c.bugExcerpts {
		if f(excerpt) {
			matching = append(matching, excerpt.Id)
		}
	}

	if len(matching) > 1 {
		return entity.UnsetId, bug.NewErrMultipleMatchBug(matching)
	}

	if len(matching) == 0 {
		return entity.UnsetId, bug.ErrBugNotExist
	}

	return matching[0], nil
}

// QueryBugs return the id of all Bug matching the given Query
func (c *RepoCache) QueryBugs(q *query.Query) []entity.Id {
	c.muBug.RLock()
	defer c.muBug.RUnlock()

	if q == nil {
		return c.AllBugsIds()
	}

	matcher := compileMatcher(q.Filters)

	var filtered []*BugExcerpt

	for _, excerpt := range c.bugExcerpts {
		if matcher.Match(excerpt, c) {
			filtered = append(filtered, excerpt)
		}
	}

	var sorter sort.Interface

	switch q.OrderBy {
	case query.OrderById:
		sorter = BugsById(filtered)
	case query.OrderByCreation:
		sorter = BugsByCreationTime(filtered)
	case query.OrderByEdit:
		sorter = BugsByEditTime(filtered)
	default:
		panic("missing sort type")
	}

	switch q.OrderDirection {
	case query.OrderAscending:
		// Nothing to do
	case query.OrderDescending:
		sorter = sort.Reverse(sorter)
	default:
		panic("missing sort direction")
	}

	sort.Sort(sorter)

	result := make([]entity.Id, len(filtered))

	for i, val := range filtered {
		result[i] = val.Id
	}

	return result
}

// AllBugsIds return all known bug ids
func (c *RepoCache) AllBugsIds() []entity.Id {
	c.muBug.RLock()
	defer c.muBug.RUnlock()

	result := make([]entity.Id, len(c.bugExcerpts))

	i := 0
	for _, excerpt := range c.bugExcerpts {
		result[i] = excerpt.Id
		i++
	}

	return result
}

// ValidLabels list valid labels
//
// Note: in the future, a proper label policy could be implemented where valid
// labels are defined in a configuration file. Until that, the default behavior
// is to return the list of labels already used.
func (c *RepoCache) ValidLabels() []bug.Label {
	c.muBug.RLock()
	defer c.muBug.RUnlock()

	set := map[bug.Label]interface{}{}

	for _, excerpt := range c.bugExcerpts {
		for _, l := range excerpt.Labels {
			set[l] = nil
		}
	}

	result := make([]bug.Label, len(set))

	i := 0
	for l := range set {
		result[i] = l
		i++
	}

	// Sort
	sort.Slice(result, func(i, j int) bool {
		return string(result[i]) < string(result[j])
	})

	return result
}

// NewBug create a new bug
// The new bug is written in the repository (commit)
func (c *RepoCache) NewBug(title string, message string) (*BugCache, *bug.CreateOperation, error) {
	return c.NewBugWithFiles(title, message, nil)
}

// NewBugWithFiles create a new bug with attached files for the message
// The new bug is written in the repository (commit)
func (c *RepoCache) NewBugWithFiles(title string, message string, files []repository.Hash) (*BugCache, *bug.CreateOperation, error) {
	return c.NewBugRawForUser(time.Now().Unix(), title, message, files, nil)
}

func (c *RepoCache) NewBugRawForUser(unixTime int64, title string, message string, files []repository.Hash, metadata map[string]string) (*BugCache, *bug.CreateOperation, error) {
	author, err := c.GetUserIdentity()
	if err != nil {
		return nil, nil, err
	}

	return c.NewBugRaw(author, unixTime, title, message, files, metadata)
}

// NewBugWithFilesMeta create a new bug with attached files for the message, as
// well as metadata for the Create operation.
// The new bug is written in the repository (commit)
func (c *RepoCache) NewBugRaw(author *IdentityCache, unixTime int64, title string, message string, files []repository.Hash, metadata map[string]string) (*BugCache, *bug.CreateOperation, error) {
	b, op, err := bug.CreateWithFiles(author.Identity, unixTime, title, message, files)
	if err != nil {
		return nil, nil, err
	}

	for key, value := range metadata {
		op.SetMetadata(key, value)
	}

	err = b.Commit(c.repo)
	if err != nil {
		return nil, nil, err
	}

	c.muBug.Lock()
	if _, has := c.bugs[b.Id()]; has {
		c.muBug.Unlock()
		return nil, nil, fmt.Errorf("bug %s already exist in the cache", b.Id())
	}

	cached := NewBugCache(c, b)
	c.bugs[b.Id()] = cached
	c.loadedBugs.Add(b.Id())
	c.muBug.Unlock()

	c.evictIfNeeded()

	// force the write of the excerpt
	err = c.bugUpdated(b.Id())
	if err != nil {
		return nil, nil, err
	}

	return cached, op, nil
}

// RemoveBug removes a bug from the cache and repo given a bug id prefix
func (c *RepoCache) RemoveBug(prefix string) error {
	c.muBug.RLock()

	b, err := c.ResolveBugPrefix(prefix)
	if err != nil {
		c.muBug.RUnlock()
		return err
	}
	c.muBug.RUnlock()

	c.muBug.Lock()
	err = bug.RemoveBug(c.repo, b.Id())

	delete(c.bugs, b.Id())
	delete(c.bugExcerpts, b.Id())
	c.loadedBugs.Remove(b.Id())

	c.muBug.Unlock()

	return c.writeBugCache()
}
