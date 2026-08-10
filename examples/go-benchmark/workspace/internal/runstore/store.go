package runstore

type State struct {
	Status string
}

type Repository interface {
	Commit(*State) error
}
