package buildinfo

import "fmt"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

type Info struct {
	Component string `json:"component"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
}

func Current(component string) Info {
	return Info{
		Component: component,
		Version:   Version,
		Commit:    Commit,
		BuildTime: BuildTime,
	}
}

func (i Info) String() string {
	return fmt.Sprintf("%s version=%s commit=%s build_time=%s", i.Component, i.Version, i.Commit, i.BuildTime)
}
