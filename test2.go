package main
import "fmt"
import "path"
import "path/filepath"
func main() {
    dst := "script.sh"
    dir := path.Dir(filepath.ToSlash(dst))
    base := path.Base(filepath.ToSlash(dst))
    if dir == "." || dir == "" {
        dir = ""
    }
    if base == "." || base == "" {
        base = "foo"
    }
    fmt.Printf("dir: %q\n", dir)
    fmt.Printf("base: %q\n", base)
}
