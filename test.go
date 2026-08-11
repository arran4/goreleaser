package main

import (
	"fmt"
	"path"
	"path/filepath"
)

func main() {
	dst := "script.sh"
	cleanedDst := filepath.ToSlash(dst)
	fmt.Printf("dir: %s\n", path.Dir(cleanedDst))
	fmt.Printf("base: %s\n", path.Base(cleanedDst))
}
