package config

// V1SkillsInfo mirrors ConfigSkillsV1.Info.
type V1SkillsInfo struct {
	Paths []string `json:"paths,omitempty"`
	URLs  []string `json:"urls,omitempty"`
}
