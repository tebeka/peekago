package main

type Node struct {
	Name string
	Next *Node
}

func main() {
	c := &Node{Name: "c"}
	b := &Node{Name: "b", Next: c}
	a := &Node{Name: "a", Next: b}
	tail := a.Next.Next
	_ = tail
}
