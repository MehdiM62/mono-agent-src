package workflow

import "sync"

// maxParallelBranches bounds how many nodes run at once when a workflow
// opts into parallel branches.
const maxParallelBranches = 4

// parallelBranchesEnabled reports whether the workflow opted into running
// independent branches concurrently (settings.parallel_branches = true).
func parallelBranchesEnabled(wf *Workflow) bool {
	if wf == nil {
		return false
	}
	v, ok := wf.Settings["parallel_branches"].(bool)
	return ok && v
}

// nextBatch returns the nodes to run together starting at order[i]: just
// order[i] when running sequentially, otherwise the longest run of
// following nodes that do not depend on one another. Loop nodes always run
// alone, since their runs change which nodes may run next.
func (r *runner) nextBatch(i int) []WorkflowNode {
	first := r.order[i]
	if r.parallel <= 1 || r.dag.isLoopNode(first.ID) {
		return r.order[i : i+1]
	}
	members := map[string]bool{first.ID: true}
	j := i + 1
	for ; j < len(r.order); j++ {
		n := r.order[j]
		if r.dag.isLoopNode(n.ID) || dependsOnAny(r.dag, n.ID, members) {
			break
		}
		members[n.ID] = true
	}
	return r.order[i:j]
}

func dependsOnAny(dag *DAG, nodeID string, members map[string]bool) bool {
	for _, c := range dag.inEdges[nodeID] {
		if members[c.SourceNodeID] {
			return true
		}
	}
	return false
}

// executeTasks runs prepared tasks — concurrently, at most r.parallel at a
// time, when there is more than one. Results land on each task; finish
// applies them afterwards in order, so routing stays deterministic and the
// runner's maps are only touched from the calling goroutine.
func (r *runner) executeTasks(tasks []*nodeTask) {
	if len(tasks) <= 1 || r.parallel <= 1 {
		for _, t := range tasks {
			r.runTask(t)
		}
		return
	}
	sem := make(chan struct{}, r.parallel)
	var wg sync.WaitGroup
	for _, t := range tasks {
		wg.Add(1)
		sem <- struct{}{}
		go func(t *nodeTask) {
			defer wg.Done()
			defer func() { <-sem }()
			r.runTask(t)
		}(t)
	}
	wg.Wait()
}
