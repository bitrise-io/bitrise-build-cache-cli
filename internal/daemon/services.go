package daemon

const LabelPrefix = "io.bitrise.build-cache."

const UnitPrefix = "bitrise-build-cache-"

type Service struct {
	Name string
	Args []string
}

func (s Service) Label() string {
	return LabelPrefix + s.Name
}

func (s Service) UnitName() string {
	return UnitPrefix + s.Name
}

func DefaultServices() []Service {
	return []Service{
		{
			Name: "xcelerate-proxy",
			Args: []string{"xcelerate", "start-proxy"},
		},
		{
			Name: "ccache-helper",
			Args: []string{"ccache", "storage-helper", "start"},
		},
	}
}
