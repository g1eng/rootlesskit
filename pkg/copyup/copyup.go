package copyup

type ChildDriver interface {
	CopyUp([]string) ([]string, error)
	CopyUpWithExclusion([]string, []string) ([]string, error)
}
