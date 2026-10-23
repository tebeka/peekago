package main

import "fmt"

var total = 1

func add(n int) int {
	total += n
	return total
}

func main() {
	x := 2
	y := add(x)
	fmt.Println(y)
}
