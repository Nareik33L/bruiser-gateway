package lease

import "testing"

func TestCanPreempt(t *testing.T) {
	browser := Principal{Type: "browser", ID: "tab"}
	agent := Principal{Type: "agent", ID: "bot"}
	other := Principal{Type: "agent", ID: "bot-2"}
	if !CanPreempt(browser, agent) {
		t.Fatal("browser should preempt agent")
	}
	if CanPreempt(agent, browser) {
		t.Fatal("agent must not preempt browser")
	}
	if CanPreempt(agent, other) {
		t.Fatal("equal rank must not preempt")
	}
	if CanPreempt(browser, browser) {
		t.Fatal("equal browser rank must not preempt")
	}
}
