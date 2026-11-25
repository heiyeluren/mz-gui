package testkit

type FakePlatform struct {
	OS          string
	Arch        string
	Permissions map[string]bool
	Features    map[string]bool
}

func (platform FakePlatform) HasPermission(name string) bool {
	return platform.Permissions[name]
}

func (platform FakePlatform) HasFeature(name string) bool {
	return platform.Features[name]
}
