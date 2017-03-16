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

package engine

import (
	"bytes"
	"fmt"
	"os"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/meta"
	"k8s.io/kubernetes/pkg/api/v1"
	app "k8s.io/kubernetes/pkg/api/v1"
	"k8s.io/kubernetes/pkg/apimachinery/announced"
	"k8s.io/kubernetes/pkg/apimachinery/registered"
	_ "k8s.io/kubernetes/pkg/apis/apps/install"
	apps "k8s.io/kubernetes/pkg/apis/apps/v1beta1"
	_ "k8s.io/kubernetes/pkg/apis/authentication/install"
	_ "k8s.io/kubernetes/pkg/apis/authorization/install"
	_ "k8s.io/kubernetes/pkg/apis/autoscaling/install"
	_ "k8s.io/kubernetes/pkg/apis/batch/install"
	batch "k8s.io/kubernetes/pkg/apis/batch/v1"
	_ "k8s.io/kubernetes/pkg/apis/certificates/install"
	_ "k8s.io/kubernetes/pkg/apis/componentconfig/install"
	_ "k8s.io/kubernetes/pkg/apis/extensions/install"
	extensions "k8s.io/kubernetes/pkg/apis/extensions/v1beta1"
	_ "k8s.io/kubernetes/pkg/apis/imagepolicy/install"
	_ "k8s.io/kubernetes/pkg/apis/policy/install"
	_ "k8s.io/kubernetes/pkg/apis/rbac/install"
	_ "k8s.io/kubernetes/pkg/apis/storage/install"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/runtime/serializer/json"
)

const (
	// defaultPathKey is the key of chart path
	defaultPathKey      = "helm.sh/pollutant"
	defaultNamespaceKey = "helm.sh/namespace"
	defaultReleaseKey   = "helm.sh/release"
	defaultRevisionKey  = "helm.sh/revision"
)

var (
	// defaultSerializer is a codec and used for encoding and decoding kubernetes resources
	defaultSerializer *json.Serializer = nil
)

func init() {
	// create default serializer
	schema := runtime.NewScheme()
	errs := []error{api.AddToScheme(schema), v1.AddToScheme(schema)}
	for _, err := range errs {
		if err != nil {
			panic(err)
		}
	}
	announced.DefaultGroupFactoryRegistry.RegisterAndEnableAll(registered.NewOrDie(os.Getenv("KUBE_API_VERSIONS")), schema)
	defaultSerializer = json.NewYAMLSerializer(json.DefaultMetaFactory, schema, schema)
}

// pollute adds pollutant to resource annotations. If resource is not a valid kubernetes
// resource, it does nothing and returns original resource.
func pollute(resource string, r *renderable) string {
	// decode object
	obj, _, err := defaultSerializer.Decode([]byte(resource), nil, nil)
	if err != nil {
		return resource
	}
	accessor := meta.NewAccessor()
	annotations, err := accessor.Annotations(obj)
	if err != nil {
		return resource
	}
	err = accessor.SetAnnotations(obj, merge(annotations, r))
	if err != nil {
		return resource
	}

	// check and pollute specific types
	switch ins := obj.(type) {
	case *extensions.Deployment:
		{
			ins.Spec.Template.Annotations = merge(ins.Spec.Template.Annotations, r)
		}
	case *extensions.DaemonSet:
		{
			ins.Spec.Template.Annotations = merge(ins.Spec.Template.Annotations, r)
		}
	case *extensions.ReplicaSet:
		{
			ins.Spec.Template.Annotations = merge(ins.Spec.Template.Annotations, r)
		}
	case *apps.StatefulSet:
		{
			ins.Spec.Template.Annotations = merge(ins.Spec.Template.Annotations, r)
		}
	case *batch.Job:
		{
			ins.Spec.Template.Annotations = merge(ins.Spec.Template.Annotations, r)
		}
	case *app.ReplicationController:
		{
			ins.Spec.Template.Annotations = merge(ins.Spec.Template.Annotations, r)
		}
	}

	// encode object
	buf := bytes.NewBuffer(nil)
	err = defaultSerializer.Encode(obj, buf)
	if err != nil {
		return resource
	}
	return buf.String()
}

// merge merges renderable info into origin.
func merge(origin map[string]string, r *renderable) map[string]string {
	if origin == nil {
		origin = make(map[string]string)
	}
	origin[defaultPathKey] = r.path
	values := r.vals.AsMap()
	rmap, ok := values["Release"]
	if !ok {
		return origin
	}
	release, ok := rmap.(map[string]interface{})
	if !ok {
		return origin
	}
	if value, ok := release["Namespace"]; ok {
		origin[defaultNamespaceKey] = fmt.Sprint(value)
	}
	if value, ok := release["Name"]; ok {
		origin[defaultReleaseKey] = fmt.Sprint(value)
	}
	if value, ok := release["Revision"]; ok {
		origin[defaultRevisionKey] = fmt.Sprint(value)
	}
	return origin
}
