/*
Copyright 2016 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package storage // import "k8s.io/helm/pkg/storage"

import (
	"fmt"
	"strings"

	rspb "k8s.io/helm/pkg/proto/hapi/release"
	relutil "k8s.io/helm/pkg/releaseutil"
	"k8s.io/helm/pkg/storage/driver"
)

// Storage represents a storage engine for a Release.
type Storage struct {
	driver.Driver
	Log func(string, ...interface{})
}

// Get retrieves the release from storage. An error is returned
// if the storage driver failed to fetch the release, or the
// release identified by the key, version pair does not exist.
func (s *Storage) Get(name string, version int32) (*rspb.Release, error) {
	namespace, name := splitName(name)
	key := makeKey(key(namespace, name), version)
	s.Log("getting release %q", key)
	return s.Driver.Get(key)
}

// Create creates a new storage entry holding the release. An
// error is returned if the storage driver failed to store the
// release, or a release with identical an key already exists.
func (s *Storage) Create(rls *rspb.Release) error {
	key := makeKey(keyForRelease(rls), rls.Version)
	s.Log("creating release %q", key)
	return s.Driver.Create(key, rls)
}

// Update update the release in storage. An error is returned if the
// storage backend fails to update the release or if the release
// does not exist.
func (s *Storage) Update(rls *rspb.Release) error {
	key := makeKey(keyForRelease(rls), rls.Version)
	s.Log("updating release %q", key)
	return s.Driver.Update(key, rls)
}

// Delete deletes the release from storage. An error is returned if
// the storage backend fails to delete the release or if the release
// does not exist.
func (s *Storage) Delete(name string, version int32) (*rspb.Release, error) {
	namespace, name := splitName(name)
	key := makeKey(key(namespace, name), version)
	s.Log("deleting release %q", key)
	return s.Driver.Delete(key)
}

// ListReleases returns all releases from storage. An error is returned if the
// storage backend fails to retrieve the releases.
func (s *Storage) ListReleases() ([]*rspb.Release, error) {
	s.Log("listing all releases in storage")
	return s.Driver.List(func(_ *rspb.Release) bool { return true })
}

// ListDeleted returns all releases with Status == DELETED. An error is returned
// if the storage backend fails to retrieve the releases.
func (s *Storage) ListDeleted() ([]*rspb.Release, error) {
	s.Log("listing deleted releases in storage")
	return s.Driver.List(func(rls *rspb.Release) bool {
		return relutil.StatusFilter(rspb.Status_DELETED).Check(rls)
	})
}

// ListDeployed returns all releases with Status == DEPLOYED. An error is returned
// if the storage backend fails to retrieve the releases.
func (s *Storage) ListDeployed() ([]*rspb.Release, error) {
	s.Log("listing all deployed releases in storage")
	return s.Driver.List(func(rls *rspb.Release) bool {
		return relutil.StatusFilter(rspb.Status_DEPLOYED).Check(rls)
	})
}

// ListFilterAll returns the set of releases satisfying satisfying the predicate
// (filter0 && filter1 && ... && filterN), i.e. a Release is included in the results
// if and only if all filters return true.
func (s *Storage) ListFilterAll(fns ...relutil.FilterFunc) ([]*rspb.Release, error) {
	s.Log("listing all releases with filter")
	return s.Driver.List(func(rls *rspb.Release) bool {
		return relutil.All(fns...).Check(rls)
	})
}

// ListFilterAny returns the set of releases satisfying satisfying the predicate
// (filter0 || filter1 || ... || filterN), i.e. a Release is included in the results
// if at least one of the filters returns true.
func (s *Storage) ListFilterAny(fns ...relutil.FilterFunc) ([]*rspb.Release, error) {
	s.Log("listing any releases with filter")
	return s.Driver.List(func(rls *rspb.Release) bool {
		return relutil.Any(fns...).Check(rls)
	})
}

// Deployed returns the deployed release with the provided release name, or
// returns ErrReleaseNotFound if not found.
func (s *Storage) Deployed(name string) (*rspb.Release, error) {
	namespace, name := splitName(name)
	key := key(namespace, name)
	s.Log("getting deployed release from %q history", key)

	ls, err := s.Driver.Query(map[string]string{
		"NAME":      name,
		"NAMESPACE": namespace,
		"OWNER":     "TILLER",
		"STATUS":    "DEPLOYED",
	})
	switch {
	case err != nil:
		return nil, err
	case len(ls) == 0:
		return nil, fmt.Errorf("%q has no deployed releases", name)
	default:
		return ls[0], nil
	}
}

// History returns the revision history for the release with the provided name, or
// returns ErrReleaseNotFound if no such release name exists.
func (s *Storage) History(name string) ([]*rspb.Release, error) {
	namespace, name := splitName(name)
	key := key(namespace, name)
	s.Log("getting release history for %q", key)

	return s.Driver.Query(map[string]string{
		"NAME":      name,
		"NAMESPACE": namespace,
		"OWNER":     "TILLER",
	})
}

// Last fetches the last revision of the named release.
func (s *Storage) Last(name string) (*rspb.Release, error) {
	s.Log("getting last revision of %q", convertName(name))
	h, err := s.History(name)
	if err != nil {
		return nil, err
	}
	if len(h) == 0 {
		return nil, fmt.Errorf("no revision for release %q", name)
	}

	relutil.Reverse(h, relutil.SortByRevision)
	return h[0], nil
}

func convertName(name string) string {
	return key(splitName(name))
}

func splitName(name string) (string, string) {
	results := strings.Split(name, "/")
	switch len(results) {
	case 0:
		return "", ""
	case 2:
		return results[0], results[1]
	default:
		return "", results[len(results)-1]
	}
}

// key generates unique name for namespace and name.
func key(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return fmt.Sprintf("%s.%s", name, namespace)
}

// keyForRelease generates unique name for release
func keyForRelease(rls *rspb.Release) string {
	_, rls.Name = splitName(rls.Name)
	return key(rls.Namespace, rls.Name)
}

// makeKey concatenates a release name and version into
// a string with format ```<release_name>#v<version>```.
// This key is used to uniquely identify storage objects.
func makeKey(rlsname string, version int32) string {
	return fmt.Sprintf("%s.v%d", rlsname, version)
}

// Init initializes a new storage backend with the driver d.
// If d is nil, the default in-memory driver is used.
func Init(d driver.Driver) *Storage {
	// default driver is in memory
	if d == nil {
		d = driver.NewMemory()
	}
	return &Storage{
		Driver: d,
		Log:    func(_ string, _ ...interface{}) {},
	}
}
