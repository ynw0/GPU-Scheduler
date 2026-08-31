package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"gpu-scheduler/internal/auth"
	"gpu-scheduler/internal/config"
	"gpu-scheduler/internal/store"
)

func main() {
	configPath := flag.String("config", "/etc/gpu-scheduler/config.toml", "path to config.toml")
	username := flag.String("username", "admin", "administrator username")
	email := flag.String("email", "", "administrator notification email")
	flag.Parse()
	cfg, err := config.Load(*configPath)
	if err != nil {
		fatal(err)
	}
	database, err := store.Open(context.Background(), cfg.Controller.DatabasePath)
	if err != nil {
		fatal(err)
	}
	defer database.Close()
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprintf(os.Stdout, "Create administrator %q? Type YES: ", *username)
	confirmation, _ := reader.ReadString('\n')
	if strings.TrimSpace(confirmation) != "YES" {
		fatal(fmt.Errorf("administrator creation canceled"))
	}
	fmt.Fprint(os.Stdout, "Password: ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stdout)
	if err != nil {
		fatal(err)
	}
	fmt.Fprint(os.Stdout, "Repeat password: ")
	repeated, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stdout)
	if err != nil {
		fatal(err)
	}
	if string(password) != string(repeated) {
		fatal(fmt.Errorf("passwords do not match"))
	}
	service := auth.NewService(database,
		time.Duration(cfg.Security.SessionIdleHours)*time.Hour,
		time.Duration(cfg.Security.SessionAbsoluteHours)*time.Hour,
		cfg.Security.PasswordMinLength)
	if err := service.BootstrapAdmin(context.Background(), *username, *email, string(password)); err != nil {
		fatal(err)
	}
	fmt.Fprintln(os.Stdout, "Administrator created.")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
