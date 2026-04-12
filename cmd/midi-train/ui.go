package main

import (
	"bufio"
	"fmt"
)

func waitEnter(reader *bufio.Reader) {
	reader.ReadString('\n')
}

func clearScreen() {
	fmt.Print("\033[2J\033[H")
}

func printHeader() {
	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║        🎛️  BOOM MIDI TRAINER  🎛️         ║")
	fmt.Println("╚══════════════════════════════════════════╝")
	fmt.Println()
}

func printSeparator() {
	fmt.Println()
	fmt.Println("────────────────────────────────────────────")
	fmt.Println()
}

func printLearnedBox(learned *LearnedControl, sampleCount int) {
	fmt.Println()
	fmt.Println("┌──────────────────────────────────────────┐")
	fmt.Printf("│ Naam:      %-29s │\n", learned.Name)
	fmt.Printf("│ Type:      %-29s │\n", learned.Type)
	fmt.Printf("│ Status:    %-29s │\n", learned.Status)
	fmt.Printf("│ Channel:   %-29d │\n", learned.Channel)
	fmt.Printf("│ Number:    %-29d │\n", learned.Number)
	fmt.Printf("│ Min:       %-29d │\n", learned.MinValue)
	fmt.Printf("│ Max:       %-29d │\n", learned.MaxValue)
	fmt.Printf("│ Berichten: %-29d │\n", sampleCount)
	fmt.Println("└──────────────────────────────────────────┘")
}

func printSavedFooter(count int, filename string) {
	fmt.Println()
	fmt.Println("════════════════════════════════════════════")
	fmt.Printf("  %d controls opgeslagen naar:\n", count)
	fmt.Printf("  %s\n", filename)
	fmt.Println("════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("Geef dit bestand aan Claude om een controller-mapping te genereren.")
	fmt.Println()
}
