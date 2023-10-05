package actions

type Control int

const (
	Errored Control = 0
	Proceed Control = 10
	Refresh Control = 20
)

func ProceedOnSuccess(err error) (Control, error) {
	if err != nil {
		return Errored, err
	}

	return Proceed, nil
}

func RefreshOnSuccess(err error) (Control, error) {
	if err != nil {
		return Errored, err
	}

	return Refresh, nil
}
