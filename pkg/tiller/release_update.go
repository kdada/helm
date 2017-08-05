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

package tiller

import (
	"fmt"

	ctx "golang.org/x/net/context"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/hooks"
	"k8s.io/helm/pkg/proto/hapi/release"
	"k8s.io/helm/pkg/proto/hapi/services"
	"k8s.io/helm/pkg/timeconv"
)

// UpdateRelease takes an existing release and new information, and upgrades the release.
func (s *ReleaseServer) UpdateRelease(c ctx.Context, req *services.UpdateReleaseRequest) (*services.UpdateReleaseResponse, error) {
	s.Log("preparing update for %s", req.Name)
	currentRelease, updatedRelease, err := s.prepareUpdate(req)
	if err != nil {
		return nil, err
	}

	s.Log("performing update for %s", req.Name)
	res, err := s.performUpdate(currentRelease, updatedRelease, req)
	if err != nil {
		return res, err
	}

	if !req.DryRun {
		s.Log("creating updated release for %s", req.Name)
		if err := s.env.Releases.Create(updatedRelease); err != nil {
			return res, err
		}
	}

	return res, nil
}

// prepareUpdate builds an updated release for an update operation.
func (s *ReleaseServer) prepareUpdate(req *services.UpdateReleaseRequest) (*release.Release, *release.Release, error) {
	if !ValidName.MatchString(req.Name) {
		return nil, nil, errMissingRelease
	}

	if req.Chart == nil {
		return nil, nil, errMissingChart
	}

	// finds the non-deleted release with the given name
	currentRelease, err := s.env.Releases.Last(req.Name)
	if err != nil {
		return nil, nil, err
	}

	// If new values were not supplied in the upgrade, re-use the existing values.
	if err := s.reuseValues(req, currentRelease); err != nil {
		return nil, nil, err
	}

	// Increment revision count. This is passed to templates, and also stored on
	// the release object.
	revision := currentRelease.Version + 1

	ts := timeconv.Now()
	options := chartutil.ReleaseOptions{
		Name:      currentRelease.Name,
		Time:      ts,
		Namespace: currentRelease.Namespace,
		IsUpgrade: true,
		Revision:  int(revision),
	}

	caps, err := capabilities(s.clientset.Discovery())
	if err != nil {
		return nil, nil, err
	}
	valuesToRender, err := chartutil.ToRenderValuesCaps(req.Chart, req.Values, options, caps)
	if err != nil {
		return nil, nil, err
	}

	hooks, manifestDoc, notesTxt, err := s.renderResources(req.Chart, valuesToRender, caps.APIVersions)
	if err != nil {
		return nil, nil, err
	}

	// Store an updated release.
	updatedRelease := &release.Release{
		Name:      currentRelease.Name,
		Namespace: currentRelease.Namespace,
		Chart:     req.Chart,
		Config:    req.Values,
		Info: &release.Info{
			FirstDeployed: currentRelease.Info.FirstDeployed,
			LastDeployed:  ts,
			Status:        &release.Status{Code: release.Status_UNKNOWN},
			Description:   "Preparing upgrade", // This should be overwritten later.
		},
		Version:     revision,
		Manifest:    manifestDoc.String(),
		Hooks:       hooks,
		Annotations: req.Annotations,
	}

	if len(notesTxt) > 0 {
		updatedRelease.Info.Status.Notes = notesTxt
	}
	err = validateManifest(s.env.KubeClient, currentRelease.Namespace, manifestDoc.Bytes())
	return currentRelease, updatedRelease, err
}

func (s *ReleaseServer) performUpdate(originalRelease, updatedRelease *release.Release, req *services.UpdateReleaseRequest) (*services.UpdateReleaseResponse, error) {
	res := &services.UpdateReleaseResponse{Release: updatedRelease}

	if req.DryRun {
		s.Log("dry run for %s", updatedRelease.Name)
		res.Release.Info.Description = "Dry run complete"
		return res, nil
	}

	// pre-upgrade hooks
	if !req.DisableHooks {
		if err := s.execHook(updatedRelease.Hooks, updatedRelease.Name, updatedRelease.Namespace, hooks.PreUpgrade, req.Timeout); err != nil {
			return res, err
		}
	} else {
		s.Log("update hooks disabled for %s", req.Name)
	}
	if err := s.ReleaseModule.Update(originalRelease, updatedRelease, req, s.env); err != nil {
		msg := fmt.Sprintf("Upgrade %q failed: %s", updatedRelease.Name, err)
		s.Log("warning: %s", msg)
		originalRelease.Info.Status.Code = release.Status_SUPERSEDED
		updatedRelease.Info.Status.Code = release.Status_FAILED
		updatedRelease.Info.Description = msg
		s.recordRelease(originalRelease, true)
		s.recordRelease(updatedRelease, false)
		return res, err
	}

	// post-upgrade hooks
	if !req.DisableHooks {
		if err := s.execHook(updatedRelease.Hooks, updatedRelease.Name, updatedRelease.Namespace, hooks.PostUpgrade, req.Timeout); err != nil {
			return res, err
		}
	}

	originalRelease.Info.Status.Code = release.Status_SUPERSEDED
	s.recordRelease(originalRelease, true)

	updatedRelease.Info.Status.Code = release.Status_DEPLOYED
	updatedRelease.Info.Description = "Upgrade complete"

	return res, nil
}
