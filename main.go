// Entry-Point to our code.

package main

import (
	"context"
	"flag"
	"os"

	"github.com/google/subcommands"
)

//
// Our main-driver.
//
// Load the sub-commands we have available, and pass control appropriately.
//
func main() {

	subcommands.Register(subcommands.HelpCommand(), "")
	subcommands.Register(subcommands.FlagsCommand(), "")
	subcommands.Register(subcommands.CommandsCommand(), "")
	subcommands.Register(&clientCmd{}, "")
	subcommands.Register(&serveCmd{}, "")
	subcommands.Register(&versionCmd{}, "")

	flag.Parse()
	ctx := context.Background()
	os.Exit(int(subcommands.Execute(ctx)))

}
