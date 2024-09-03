package ctrl

type StunResolver interface {
	Resolve(port uint16) (string, int, error)
}
