package ctrl

type Serializer interface {
	Serialize(address string, port int) (string, error)
}

type Deserializer interface {
	Deserialize(data string) (string, int, error)
}
