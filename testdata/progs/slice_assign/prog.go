package main

func main() {
	xs := []int{1, 2, 3}
	ys := xs
	xs = append(xs, 4)
	xs[0] = 99

	_ = xs
	_ = ys
}
