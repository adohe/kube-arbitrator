/*
Copyright 2017 The Kubernetes Authors.

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

package scheduler

import (
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/actions/allocate"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/actions/decorate"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/actions/garantee"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/framework"

	// Import drf plugins
	_ "github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/plugins/drf"
)

// Actions is a list of action that should be executed in order.
var Actions = []framework.Action{
	decorate.New(),
	garantee.New(),
	allocate.New(),
}
