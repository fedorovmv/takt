package runstore

func Complete(repo Repository, state *State) error {
	state.Status = "completed"
	_ = repo.Commit(state)
	return nil
}
