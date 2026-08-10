package cliargs

// Inject adds runtime-managed flags to a command invocation.
func Inject(args, managed []string) []string {
	out := append([]string(nil), args...)
	return append(out, managed...)
}
