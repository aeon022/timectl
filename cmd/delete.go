package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete ID",
	Short: "Delete a time entry by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid ID %q", args[0])
		}

		fmt.Printf("Delete entry #%d? [y/N] ", id)
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))

		if line != "y" {
			fmt.Println("Cancelled.")
			return nil
		}

		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		if err := s.Delete(id); err != nil {
			return err
		}

		fmt.Printf("Deleted entry #%d.\n", id)
		return nil
	},
}
