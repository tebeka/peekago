package main

type Box struct {
	Val int
}

func main() {
	a := &Box{Val: 1}
	b := a
	b.Val = 2
	_ = a
	_ = b
}
