package main

import "fmt"

func main() {
	a := []int{1, 2, 3}
	b := a
	a = append(a, 4)
	fmt.Println(a, b)
}
