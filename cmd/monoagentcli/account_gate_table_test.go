package main

import "github.com/monoes/mono-agent/internal/cli/accountgate"

// accountClasses is the gate's classification table, for the tests here that hold it to the real
// command tree.
var accountClasses = accountgate.Listed()
