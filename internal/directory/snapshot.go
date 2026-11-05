package directory

import (
	"maps"
	"slices"
)

func copyValue[T any](p *T) *T {
	if p == nil {
		return nil
	}
	value := *p
	return &value
}

// Snapshot mutable collections and pointers, sharing immutable string storage.
// JSON round trips duplicate every script/output and build large encoding buffers
// on each device poll, even when no state changes.
func copyJobState(st State) State {
	next := st
	next.Users = maps.Clone(st.Users)
	next.Groups = maps.Clone(st.Groups)
	for name, members := range next.Groups {
		next.Groups[name] = slices.Clone(members)
	}
	next.Devices = maps.Clone(st.Devices)
	for id, device := range next.Devices {
		device.AllowedUsers = slices.Clone(device.AllowedUsers)
		device.AllowedGroups = slices.Clone(device.AllowedGroups)
		device.LastJobPoll = copyValue(device.LastJobPoll)
		next.Devices[id] = device
	}
	next.Jobs = make(map[string]Job, len(st.Jobs))
	for id, job := range st.Jobs {
		job.DeviceIDs = slices.Clone(job.DeviceIDs)
		job.NextRunAt = copyValue(job.NextRunAt)
		next.Jobs[id] = job
	}
	next.Runs = make(map[string]JobRun, len(st.Runs))
	for id, run := range st.Runs {
		run.StartedAt = copyValue(run.StartedAt)
		run.FinishedAt = copyValue(run.FinishedAt)
		run.LeaseUntil = copyValue(run.LeaseUntil)
		run.ExitCode = copyValue(run.ExitCode)
		next.Runs[id] = run
	}
	return next
}
