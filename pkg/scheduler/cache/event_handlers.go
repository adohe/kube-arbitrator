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

package cache

import (
	"fmt"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1beta1"
	"k8s.io/client-go/tools/cache"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/utils"
	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1alpha1"
	arbapi "github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/api"
)

func isTerminated(status arbapi.TaskStatus) bool {
	return status == arbapi.Succeeded || status == arbapi.Failed
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) addPod(pod *v1.Pod) error {
	pi := arbapi.NewTaskInfo(pod)

	if len(pi.Job) != 0 {
		if _, found := sc.Jobs[pi.Job]; !found {
			sc.Jobs[pi.Job] = arbapi.NewJobInfo(pi.Job)
		}

		// TODO(k82cn): it's found that the Add event will be sent
		// multiple times without update/delete. That should be a
		// client-go issue, we need to dig deeper for that.
		sc.Jobs[pi.Job].DeleteTaskInfo(pi)
		sc.Jobs[pi.Job].AddTaskInfo(pi)
	} else {
		glog.Warningf("The controller of pod %v/%v is empty, can not schedule it.",
			pod.Namespace, pod.Name)
	}

	if len(pi.NodeName) != 0 {
		glog.V(3).Infof("Add task %v/%v into host %v", pi.Namespace, pi.Name, pi.NodeName)

		if _, found := sc.Nodes[pi.NodeName]; !found {
			sc.Nodes[pi.NodeName] = arbapi.NewNodeInfo(nil)
		}

		node := sc.Nodes[pi.NodeName]
		node.RemoveTask(pi)

		if !isTerminated(pi.Status) {
			node.AddTask(pi)
		}
	}

	return nil
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) updatePod(oldPod, newPod *v1.Pod) error {
	if err := sc.deletePod(oldPod); err != nil {
		return err
	}
	return sc.addPod(newPod)
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) deletePod(pod *v1.Pod) error {
	pi := arbapi.NewTaskInfo(pod)

	if len(pi.Job) != 0 {
		if job, found := sc.Jobs[pi.Job]; found {
			job.DeleteTaskInfo(pi)
		} else {
			glog.Warningf("Failed to find Job for Task %v:%v/%v.",
				pi.UID, pi.Namespace, pi.Name)
		}
	}

	if len(pi.NodeName) != 0 {
		node := sc.Nodes[pi.NodeName]
		if node != nil {
			glog.V(3).Infof("Delete task %v/%v from host %v", pi.Namespace, pi.Name, pi.NodeName)
			node.RemoveTask(pi)
		}
	}

	return nil
}

func (sc *SchedulerCache) AddPod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		glog.Errorf("Cannot convert to *v1.Pod: %v", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Add pod(%s) into cache, status (%s)", pod.Name, pod.Status.Phase)
	err := sc.addPod(pod)
	if err != nil {
		glog.Errorf("Failed to add pod %s into cache: %v", pod.Name, err)
		return
	}
	return
}

func (sc *SchedulerCache) UpdatePod(oldObj, newObj interface{}) {
	oldPod, ok := oldObj.(*v1.Pod)
	if !ok {
		glog.Errorf("Cannot convert oldObj to *v1.Pod: %v", oldObj)
		return
	}
	newPod, ok := newObj.(*v1.Pod)
	if !ok {
		glog.Errorf("Cannot convert newObj to *v1.Pod: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Update oldPod(%s) status(%s) newPod(%s) status(%s) in cache", oldPod.Name, oldPod.Status.Phase, newPod.Name, newPod.Status.Phase)
	err := sc.updatePod(oldPod, newPod)
	if err != nil {
		glog.Errorf("Failed to update pod %v in cache: %v", oldPod.Name, err)
		return
	}
	return
}

func (sc *SchedulerCache) DeletePod(obj interface{}) {
	var pod *v1.Pod
	switch t := obj.(type) {
	case *v1.Pod:
		pod = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		pod, ok = t.Obj.(*v1.Pod)
		if !ok {
			glog.Errorf("Cannot convert to *v1.Pod: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("Cannot convert to *v1.Pod: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Delete pod(%s) status(%s) from cache", pod.Name, pod.Status.Phase)
	err := sc.deletePod(pod)
	if err != nil {
		glog.Errorf("Failed to delete pod %v from cache: %v", pod.Name, err)
		return
	}
	return
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) addNode(node *v1.Node) error {
	if sc.Nodes[node.Name] != nil {
		sc.Nodes[node.Name].SetNode(node)
	} else {
		sc.Nodes[node.Name] = arbapi.NewNodeInfo(node)
	}

	return nil
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) updateNode(oldNode, newNode *v1.Node) error {
	// Did not delete the old node, just update related info, e.g. allocatable.
	if sc.Nodes[newNode.Name] != nil {
		sc.Nodes[newNode.Name].SetNode(newNode)
		return nil
	}

	return fmt.Errorf("node <%s> does not exist", newNode.Name)
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) deleteNode(node *v1.Node) error {
	if _, ok := sc.Nodes[node.Name]; !ok {
		return fmt.Errorf("node <%s> does not exist", node.Name)
	}
	delete(sc.Nodes, node.Name)
	return nil
}

func (sc *SchedulerCache) AddNode(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		glog.Errorf("Cannot convert to *v1.Node: %v", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Add node(%s) into cache", node.Name)
	err := sc.addNode(node)
	if err != nil {
		glog.Errorf("Failed to add node %s into cache: %v", node.Name, err)
		return
	}
	return
}

func (sc *SchedulerCache) UpdateNode(oldObj, newObj interface{}) {
	oldNode, ok := oldObj.(*v1.Node)
	if !ok {
		glog.Errorf("Cannot convert oldObj to *v1.Node: %v", oldObj)
		return
	}
	newNode, ok := newObj.(*v1.Node)
	if !ok {
		glog.Errorf("Cannot convert newObj to *v1.Node: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Update oldNode(%s) newNode(%s) in cache", oldNode.Name, newNode.Name)
	err := sc.updateNode(oldNode, newNode)
	if err != nil {
		glog.Errorf("Failed to update node %v in cache: %v", oldNode.Name, err)
		return
	}
	return
}

func (sc *SchedulerCache) DeleteNode(obj interface{}) {
	var node *v1.Node
	switch t := obj.(type) {
	case *v1.Node:
		node = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		node, ok = t.Obj.(*v1.Node)
		if !ok {
			glog.Errorf("Cannot convert to *v1.Node: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("Cannot convert to *v1.Node: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Delete node(%s) from cache", node.Name)
	err := sc.deleteNode(node)
	if err != nil {
		glog.Errorf("Failed to delete node %s from cache: %v", node.Name, err)
		return
	}
	return
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) setSchedulingSpec(ss *arbv1.SchedulingSpec) error {
	job := arbapi.JobID(utils.GetController(ss))

	if len(job) == 0 {
		return fmt.Errorf("the controller of SchedulingSpec is empty")
	}

	if _, found := sc.Jobs[job]; !found {
		sc.Jobs[job] = arbapi.NewJobInfo(job)
	}

	sc.Jobs[job].SetSchedulingSpec(ss)

	return nil
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) updateSchedulingSpec(oldQueue, newQueue *arbv1.SchedulingSpec) error {
	return sc.setSchedulingSpec(newQueue)
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) deleteSchedulingSpec(queue *arbv1.SchedulingSpec) error {
	return nil
}

func (sc *SchedulerCache) AddSchedulingSpec(obj interface{}) {
	ss, ok := obj.(*arbv1.SchedulingSpec)
	if !ok {
		glog.Errorf("Cannot convert to *arbv1.Queue: %v", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Add SchedulingSpec(%s) into cache, spec(%#v)", ss.Name, ss.Spec)
	err := sc.setSchedulingSpec(ss)
	if err != nil {
		glog.Errorf("Failed to add SchedulingSpec %s into cache: %v", ss.Name, err)
		return
	}
	return
}

func (sc *SchedulerCache) UpdateSchedulingSpec(oldObj, newObj interface{}) {
	oldSS, ok := oldObj.(*arbv1.SchedulingSpec)
	if !ok {
		glog.Errorf("Cannot convert oldObj to *arbv1.SchedulingSpec: %v", oldObj)
		return
	}
	newSS, ok := newObj.(*arbv1.SchedulingSpec)
	if !ok {
		glog.Errorf("Cannot convert newObj to *arbv1.SchedulingSpec: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Update oldSchedulingSpec(%s) in cache, spec(%#v)", oldSS.Name, oldSS.Spec)
	glog.V(4).Infof("Update newSchedulingSpec(%s) in cache, spec(%#v)", newSS.Name, newSS.Spec)
	err := sc.updateSchedulingSpec(oldSS, newSS)
	if err != nil {
		glog.Errorf("Failed to update SchedulingSpec %s into cache: %v", oldSS.Name, err)
		return
	}
	return
}

func (sc *SchedulerCache) DeleteSchedulingSpec(obj interface{}) {
	var ss *arbv1.SchedulingSpec
	switch t := obj.(type) {
	case *arbv1.SchedulingSpec:
		ss = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		ss, ok = t.Obj.(*arbv1.SchedulingSpec)
		if !ok {
			glog.Errorf("Cannot convert to *arbv1.SchedulingSpec: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("Cannot convert to *arbv1.SchedulingSpec: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	err := sc.deleteSchedulingSpec(ss)
	if err != nil {
		glog.Errorf("Failed to delete SchedulingSpec %s from cache: %v", ss.Name, err)
		return
	}
	return
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) setPDB(pdb *policyv1.PodDisruptionBudget) error {
	job := arbapi.JobID(utils.GetController(pdb))

	if len(job) == 0 {
		return fmt.Errorf("the controller of SchedulingSpec is empty")
	}

	if _, found := sc.Jobs[job]; !found {
		sc.Jobs[job] = arbapi.NewJobInfo(job)
	}

	sc.Jobs[job].SetPDB(pdb)

	return nil
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) updatePDB(oldQueue, newQueue *policyv1.PodDisruptionBudget) error {
	return sc.setPDB(newQueue)
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) deletePDB(queue *policyv1.PodDisruptionBudget) error {
	return nil
}

func (sc *SchedulerCache) AddPDB(obj interface{}) {
	pdb, ok := obj.(*policyv1.PodDisruptionBudget)
	if !ok {
		glog.Errorf("Cannot convert to *policyv1.PodDisruptionBudget: %v", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Add PodDisruptionBudget(%s) into cache, spec(%#v)", pdb.Name, pdb.Spec)
	err := sc.setPDB(pdb)
	if err != nil {
		glog.Errorf("Failed to add PodDisruptionBudget %s into cache: %v", pdb.Name, err)
		return
	}
	return
}

func (sc *SchedulerCache) UpdatePDB(oldObj, newObj interface{}) {
	oldPDB, ok := oldObj.(*policyv1.PodDisruptionBudget)
	if !ok {
		glog.Errorf("Cannot convert oldObj to *policyv1.PodDisruptionBudget: %v", oldObj)
		return
	}
	newPDB, ok := newObj.(*policyv1.PodDisruptionBudget)
	if !ok {
		glog.Errorf("Cannot convert newObj to *policyv1.PodDisruptionBudget: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Update oldPDB(%s) in cache, spec(%#v)", oldPDB.Name, oldPDB.Spec)
	glog.V(4).Infof("Update newPDB(%s) in cache, spec(%#v)", newPDB.Name, newPDB.Spec)
	err := sc.updatePDB(oldPDB, newPDB)
	if err != nil {
		glog.Errorf("Failed to update PodDisruptionBudget %s into cache: %v", oldPDB.Name, err)
		return
	}
	return
}

func (sc *SchedulerCache) DeletePDB(obj interface{}) {
	var pdb *policyv1.PodDisruptionBudget
	switch t := obj.(type) {
	case *policyv1.PodDisruptionBudget:
		pdb = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		pdb, ok = t.Obj.(*policyv1.PodDisruptionBudget)
		if !ok {
			glog.Errorf("Cannot convert to *policyv1.PodDisruptionBudget: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("Cannot convert to *policyv1.PodDisruptionBudget: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	err := sc.deletePDB(pdb)
	if err != nil {
		glog.Errorf("Failed to delete PodDisruptionBudget %s from cache: %v", pdb.Name, err)
		return
	}
	return
}
