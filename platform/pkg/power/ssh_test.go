package power

import "testing"

func TestShutdownCommandForRoot(t *testing.T) {
	t.Parallel()

	got := shutdownCommand("root")
	want := "sh -c 'if command -v shutdown >/dev/null 2>&1; then shutdown now; else systemctl poweroff; fi'"
	if got != want {
		t.Fatalf("shutdownCommand(root) = %q, want %q", got, want)
	}
}

func TestShutdownCommandForNonRoot(t *testing.T) {
	t.Parallel()

	got := shutdownCommand("operator")
	want := "sudo -n sh -c 'if command -v shutdown >/dev/null 2>&1; then shutdown now; else systemctl poweroff; fi'"
	if got != want {
		t.Fatalf("shutdownCommand(non-root) = %q, want %q", got, want)
	}
}

func TestValidationCommandForRoot(t *testing.T) {
	t.Parallel()

	got := validationCommand("root")
	want := "sh -c 'if command -v shutdown >/dev/null 2>&1 || command -v systemctl >/dev/null 2>&1; then true; else exit 1; fi'"
	if got != want {
		t.Fatalf("validationCommand(root) = %q, want %q", got, want)
	}
}

func TestValidationCommandForNonRoot(t *testing.T) {
	t.Parallel()

	got := validationCommand("operator")
	want := "sudo -n sh -c 'if command -v shutdown >/dev/null 2>&1 || command -v systemctl >/dev/null 2>&1; then true; else exit 1; fi'"
	if got != want {
		t.Fatalf("validationCommand(non-root) = %q, want %q", got, want)
	}
}
