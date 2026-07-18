package readcache

type Metrics interface {
	Record(outcome string)
}
