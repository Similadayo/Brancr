package persona

type Formatter interface {
	Format(input string) string
}

// FormatterFunc allows using a function as a Formatter
type FormatterFunc func(string) string

func (f FormatterFunc) Format(input string) string {
	return f(input)
}
