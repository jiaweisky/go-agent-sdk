package schema

type PositiveInt int

type NonNegativeInt int

type RelativePath string

func (p RelativePath) String() string {
	return string(p)
}

type AbsolutePath string

func (p AbsolutePath) String() string {
	return string(p)
}
