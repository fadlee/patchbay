//go:build windows

package main

import (
	"fmt"
	"os/exec"
)

func firewallRuleName(ruleID string) string {
	return "patchbay-" + ruleID
}

// ensureFirewallRule opens the Windows Firewall for a rule's listen port,
// for whichever protocol(s) the rule uses. Requires the process to be
// running elevated (as Administrator) — when installed as a service this is
// automatic (it runs as LocalSystem); in interactive/tray mode the user
// needs to have launched patchbay.exe as Administrator for this to succeed.
func ensureFirewallRule(r Rule) error {
	var protocols []string
	switch r.Protocol {
	case "udp":
		protocols = []string{"UDP"}
	case "tcp+udp":
		protocols = []string{"TCP", "UDP"}
	default:
		protocols = []string{"TCP"}
	}

	name := firewallRuleName(r.ID)
	// Clear any existing rule with this name first so re-applying (e.g. on
	// every service start) doesn't accumulate duplicates.
	removeFirewallRule(r.ID)

	for _, proto := range protocols {
		cmd := exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
			"name="+name,
			"dir=in",
			"action=allow",
			"protocol="+proto,
			fmt.Sprintf("localport=%d", r.ListenPort),
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("netsh add rule (%s): %v: %s", proto, err, string(out))
		}
	}
	return nil
}

// removeFirewallRule deletes any firewall rule previously created for this
// rule ID. Safe to call even if no such rule exists.
func removeFirewallRule(ruleID string) error {
	name := firewallRuleName(ruleID)
	cmd := exec.Command("netsh", "advfirewall", "firewall", "delete", "rule", "name="+name)
	// netsh exits non-zero with "No rules match the specified criteria"
	// when there's nothing to delete — not a real error for our purposes,
	// so this is intentionally not surfaced to callers.
	_, _ = cmd.CombinedOutput()
	return nil
}
