// Command warmup imports packages traced programs commonly use, the Dockerfile builds it to fill the image build cache.
package main

import (
	"fmt"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
)

func main() {
	fmt.Println(os.Args, strings.ToUpper(""), slices.Max([]int{1}), sort.IntsAreSorted(nil), http.StatusOK)
}
