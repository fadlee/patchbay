//go:build !windows

package main

func ensureFirewallRule(r Rule) error        { return nil }
func removeFirewallRule(ruleID string) error { return nil }
