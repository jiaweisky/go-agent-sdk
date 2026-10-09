package schema

type ProjectID string

func (id ProjectID) String() string {
	return string(id)
}

const ProjectIDGlobal ProjectID = "global"
