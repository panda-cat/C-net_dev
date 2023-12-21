package main

import (
    "bufio"
    "context"
    "encoding/csv"
    "flag"
    "fmt"
    "io"
    "log"
    "os"
    "os/exec"
    "regexp"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/golang/crypto/tree/master/ssh"
    "github.com/golang/crypto/tree/master/ssh/terminal"
)

var (
    excelFile  = flag.String("excel", "", "Excel file containing device information")
    threads    = flag.Int("threads", 4, "Number of concurrent connections")
    outputDir  = flag.String("output", "./result", "Directory to store output files")
    failedFile = flag.String("failed", "failed_devices.txt", "File to store failed devices")
)

// DeviceInfo represents a device's information
type DeviceInfo struct {
    IP          string
    Username    string
    Password    string
    DeviceType  string
    Secret      string
    ReadTimeout int
    Commands    []string
}

func main() {
    flag.Parse()

    if *excelFile == "" {
        log.Fatal("Excel file not specified")
    }

    devices, err := loadExcel(*excelFile)
    if err != nil {
        log.Fatalf("Error loading excel file: %v", err)
    }

    // Create output directory if it doesn't exist
    if err := os.MkdirAll(*outputDir, os.ModePerm); err != nil {
        log.Fatalf("Error creating output directory: %v", err)
    }

    // Create failed devices file if it doesn't exist
    failed, err := os.Create(*failedFile)
    if err != nil {
        log.Fatalf("Error creating failed devices file: %v", err)
    }
    defer failed.Close()

    // Create a channel to communicate between goroutines
    results := make(chan *DeviceInfo, len(devices))
    ctx, cancel := context.WithCancel(context.Background())

    // Start goroutines to handle device connections
    var wg sync.WaitGroup
    for i := 0; i < *threads; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for device := range results {
                if err := executeCommands(ctx, device, failed); err != nil {
                    log.Printf("Error executing commands on device %s: %v", device.IP, err)
                }
            }
        }()
    }

    // Send devices to the channel for processing
    for _, device := range devices {
        results <- device
    }
    close(results)

    // Wait for all goroutines to finish
    wg.Wait()

    // Cancel the context to stop any remaining goroutines
    cancel()
}

// loadExcel loads device information from an excel file
func loadExcel(filename string) ([]*DeviceInfo, error) {
    f, err := os.Open(filename)
    if err != nil {
        return nil, err
    }
    defer f.Close()

    csvReader := csv.NewReader(bufio.NewReader(f))
    records, err := csvReader.ReadAll()
    if err != nil {
        return nil, err
    }

    var devices []*DeviceInfo
    for _, record := range records {
        device := &DeviceInfo{
            IP:          record[0],
            Username:    record[1],
            Password:    record[2],
            DeviceType:  record[3],
            Secret:      record[4],
            ReadTimeout: 30,
            Commands:    strings.Split(record[5], ";"),
        }

        if readTimeout, err := strconv.Atoi(record[6]); err == nil {
            device.ReadTimeout = readTimeout
        }

        devices = append(devices, device)
    }

    return devices, nil
}

// executeCommands executes commands on a device
func executeCommands(ctx context.Context, device *DeviceInfo, failed *os.File) error {
    // Set up SSH client configuration
    config := &ssh.ClientConfig{
        User: device.Username,
        Auth: []ssh.AuthMethod{
            ssh.Password(device.Password),
        },
        Timeout:         10 * time.Second,
        HostKeyCallback: ssh.InsecureIgnoreHostKey(),
    }

    // Connect to the device
    conn, err := ssh.Dial("tcp", device.IP, config)
    if err != nil {
        log.Printf("Error connecting to device %s: %v", device.IP, err)
        fmt.Fprintf(failed, "%s: Error connecting\n", device.IP)
        return err
    }
    defer conn.Close()

    // Create a session for issuing commands
    session, err := conn.NewSession()
    if err != nil {
        log.Printf("Error creating session on device %s: %v", device.IP, err)
        fmt.Fprintf(failed, "%s: Error creating session\n", device.IP)
        return err
    }
    defer session.Close()

    // Set read timeout
    modes := ssh.TerminalModes{
        ssh.ReadTimeout: time.Duration(device.ReadTimeout) * time.Second,
    }
    if err := session.RequestPty("xterm", 80, 24, modes); err != nil {
        log.Printf("Error setting read timeout on device %s: %v", device.IP, err)
        fmt.Fprintf(failed, "%s: Error setting read timeout\n", device.IP)
        return err
    }

    // Enable interactive mode
    state, err := terminal.MakeRaw(int(os.Stdin.Fd()))
    if err != nil {
        log.Printf("Error enabling interactive mode on device %s: %v", device.IP, err)
        fmt.Fprintf(failed, "%s: Error enabling interactive mode\n", device.IP)
        return err
    }
    defer terminal.Restore(int(os.Stdin.Fd()), state)

    // Send commands to the device
    var output []byte
    for _, command := range device.Commands {
        // Send command and read output
        if err := session.Write([]byte(command + "\n")); err != nil {
            log.Printf("Error sending command to device %s: %v", device.IP, err)
            fmt.Fprintf(failed, "%s: Error sending command\n", device.IP)
            return err
        }
        
        b, err := session.ReadUntil("> ")
        if err != nil {
            log.Printf("Error reading output from device %s: %v", device.IP, err)
            fmt.Fprintf(failed, "%s: Error reading output\n", device.IP)
            return err
        }
        output = append(output, b...)
    }

    // Save output to file
    filename := fmt.Sprintf("%s/%s.txt", *outputDir, device.IP)
    if err := os.WriteFile(filename, output, 0644); err != nil {
        log.Printf("Error saving output for device %s: %v", device.IP, err)
        fmt.Fprintf(failed, "%s: Error saving output\n", device.IP)
        return err
    }

    fmt.Printf("Commands executed successfully on device %s\n", device.IP)
    return nil
}
