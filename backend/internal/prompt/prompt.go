// Package prompt reads the answers to the confirmations of the commands that
// change data (import, delete, export, restore, compact)
package prompt

import (
	"bufio"
	"io"
	"strings"
)

// Confirmed reads one answer from in and reports whether it is "y" or
// "yes", in any case. An answer at the end of the input without a newline
// counts; no answer is a no.
func Confirmed(in io.Reader) bool {
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && answer == "" {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}
