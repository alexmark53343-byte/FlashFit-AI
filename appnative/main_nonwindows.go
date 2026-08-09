//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"flashfitai/shared"
)

func main() {
	root := filepath.Join(os.TempDir(), "flashfit-native-selftest")
	if err := shared.RunSelfTest(root); err != nil {
		fmt.Fprintln(os.Stderr, "self-test failed:", err)
		os.Exit(1)
	}
	fmt.Println("FlashFit AI native self-test OK")
}
