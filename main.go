package main

import (
	"fmt"
	"os/exec"
)

func main() {
	fmt.Println("Hello, World!")
	cmd := exec.Command("stockfish")
	fmt.Println("instance:", cmd)
}
