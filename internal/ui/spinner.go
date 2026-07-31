package ui

import (
	"fmt"
	"time"
)

var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

type Spinner struct {
	stop chan struct{}
	done chan struct{}
}

func StartSpinner(label string) *Spinner {
	s := &Spinner{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		for {
			select {
			case <-s.stop:
				fmt.Print("\r\033[K") // limpia la línea
				return
			case <-ticker.C:
				fmt.Printf("\r%c %s", spinnerFrames[i], label)
				i = (i + 1) % len(spinnerFrames)
			}
		}
	}()
	return s
}

func (s *Spinner) Stop() {
	close(s.stop)
	<-s.done // bloquea hasta que la goroutine confirme que limpió la línea
}
