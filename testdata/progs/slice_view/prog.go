package main

func main() {
	xs := []int{1, 2, 3, 4, 5}
	ys := xs[1:3]
	xs = append(xs, 6)
	xs[0] = 99

	_ = xs
	_ = ys
}
