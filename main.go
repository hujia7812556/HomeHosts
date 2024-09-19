package main

import (
    "HomeHosts/config"
    "flag"
    "fmt"
    "gopkg.in/yaml.v3"
    "os"
    "os/exec"
    "runtime"
    "slices"
    "strings"
    "time"
)

const (
    hostsFilePath        = "/etc/hosts"
    homeHostsStartLine   = "# --- HOMEHOSTS_CONTENT_START ---"
    homeHostsEndLine     = "# --- HOMEHOSTS_CONTENT_END ---"
    switchHostsStartLine = "# --- SWITCHHOSTS_CONTENT_START ---"
)

var serviceType = flag.String("s", "run", "Service management (install|uninstall|restart|run|modify|restore)")
var every = flag.Int("f", 300, "Update frequency(seconds)")
var configFilePath = flag.String("c", getDefaultConfigFilePath(), "home hosts configuration file path")
var myConfig = config.Config{}

func main() {
    flag.Parse()

    var err error
    myConfig, err = loadConfig(*configFilePath)
    if err != nil {
        fmt.Println("Error loading config:", err)
        return
    }

    switch *serviceType {
    case "install":
        installLaunchAgent()
        return
    case "uninstall":
        uninstallLaunchAgent()
        return
    case "modify":
        modifyHostsFile(&myConfig)
        return
    case "restore":
        restoreHostsFile()
        return
    default:
        run(&myConfig)
        return
    }
}

func run(myConfig *config.Config) {
    //记录上次动作，防止重复执行
    lastAction := ""
    for {
        fmt.Println("\n\n[" + time.Now().Format("2006-01-02 15:04:05") + "]")
        if isConnectedToWiFi(myConfig.SSIDs) {
            if lastAction != "modify" {
                fmt.Println("start modify host...")
                modifyHostsFile(myConfig)
                lastAction = "modify"
            } else {
                fmt.Println("hosts has modified, skip.")
            }
        } else {
            if lastAction != "restore" {
                fmt.Println("start restore host...")
                restoreHostsFile()
                lastAction = "restore"
            } else {
                fmt.Println("hosts has restored, skip.")
            }
        }
        time.Sleep(time.Duration(*every) * time.Second)
    }
}

func isConnectedToWiFi(ssids []string) bool {
    ssid, err := getSSID()
    fmt.Println("SSID: " + ssid)
    if err != nil {
        fmt.Println("Error checking Wi-Fi connection:", err)
        return false
    }
    return slices.Contains(ssids, ssid)
}

func getSSID() (string, error) {
    switch runtime.GOOS {
    case "darwin":
        return getSSIDMacOS()
    //case "windows":
    //    return getSSIDWindows()
    //case "linux":
    //    return getSSIDLinux()
    default:
        return "", fmt.Errorf("unsupported platform")
    }
}

func getSSIDMacOS() (string, error) {
    out, err := exec.Command("networksetup", "-getairportnetwork", "en0").Output()
    if err != nil {
        fmt.Println("Error checking Wi-Fi connection:", err)
        return "", err
    }
    outStr := strings.TrimSpace(string(out))
    if strings.HasPrefix(outStr, "Current Wi-Fi Network: ") {
        ssid := strings.TrimPrefix(outStr, "Current Wi-Fi Network: ")
        return ssid, nil
    } else {
        return "", fmt.Errorf(outStr)
    }
}

//这里要注意兼容SwitchHosts，不要相互覆盖
func modifyHostsFile(myConfig *config.Config) {
    if len(myConfig.Hosts) == 0 {
        fmt.Println("home hosts is empty, return.")
        return
    }
    originalHosts, err := readOriginalHosts()
    if err != nil {
        fmt.Println("Error reading hosts file:", err)
        return
    }

    if isContainHomeHosts(&originalHosts) {
        fmt.Println("已有home hosts，无需修改.")
        return
    }

    isContainSwitchHosts, switchHostsIndex := searchSwitchHosts(&originalHosts)
    insertHosts := []string{"", homeHostsStartLine, ""}
    insertHosts = append(insertHosts, myConfig.Hosts...)
    insertHosts = append(insertHosts, "", homeHostsEndLine, "")

    var newHosts []string
    if isContainSwitchHosts {
        newHosts = append(originalHosts[:switchHostsIndex-1], append(insertHosts, originalHosts[switchHostsIndex-1:]...)...)
    } else {
        newHosts = append(originalHosts, insertHosts...)
    }

    // 追加新内容到 hosts 文件
    hostsFile, err := os.OpenFile(hostsFilePath, os.O_TRUNC|os.O_WRONLY, 0644)
    if err != nil {
        fmt.Println("Error opening hosts file:", err)
        return
    }
    defer hostsFile.Close()
    if _, err := hostsFile.WriteString(strings.Join(newHosts, "\n")); err != nil {
        fmt.Println("Error writing to hosts file:", err)
    } else {
        fmt.Println("Hosts file modified.")
    }
}

func restoreHostsFile() {
    originalHosts, err := readOriginalHosts()
    if err != nil {
        fmt.Println("Error reading hosts file:", err)
        return
    }

    hasHomeHosts, start, end := searchHomeHosts(&originalHosts)
    if !hasHomeHosts {
        fmt.Println("home hosts不存在，无需修改.")
        return
    }
    newHosts := append(originalHosts[:start-1], originalHosts[end:]...)
    // 追加新内容到 hosts 文件
    hostsFile, err := os.OpenFile(hostsFilePath, os.O_TRUNC|os.O_WRONLY, 0644)
    if err != nil {
        fmt.Println("Error opening hosts file:", err)
        return
    }
    defer hostsFile.Close()
    if _, err := hostsFile.WriteString(strings.Join(newHosts, "\n")); err != nil {
        fmt.Println("Error writing to hosts file:", err)
    } else {
        fmt.Println("Hosts file restored.")
    }
}

func loadConfig(configFilePath string) (config.Config, error) {
    var myConfig config.Config
    configFile, err := os.ReadFile(configFilePath)
    if err != nil {
        fmt.Print(err)
        return myConfig, err
    }
    err1 := yaml.Unmarshal(configFile, &myConfig)
    if err1 != nil {
        fmt.Println(err1)
        return myConfig, err1
    }
    return myConfig, nil
}

func readOriginalHosts() ([]string, error) {
    data, err := os.ReadFile(hostsFilePath)
    if err != nil {
        return []string{}, err
    }
    return strings.Split(string(data), "\n"), nil
}

//是否已有HomeHosts内容
func isContainHomeHosts(originHosts *[]string) bool {
    // 遍历文件中的每一行
    for _, line := range *originHosts {
        if strings.Contains(line, homeHostsStartLine) {
            return true
        }
    }
    return false
}

//查找swicthHosts配置所在的行下标，从1开始
func searchSwitchHosts(originHosts *[]string) (bool, int) {
    // 遍历文件中的每一行
    for index, line := range *originHosts {
        if strings.Contains(line, switchHostsStartLine) {
            //如果前面一行是空字符串，返回前面一行的下标
            if index > 0 && (*originHosts)[index-1] == "" {
                return true, index
            } else {
                return true, index + 1
            }
        }
    }
    return false, 0
}

//查找homeHosts配置所在开始和结束的行下标，从1开始
func searchHomeHosts(originHosts *[]string) (bool, int, int) {
    // 遍历文件中的每一行
    var start, end int
    for index, line := range *originHosts {
        if strings.Contains(line, homeHostsStartLine) {
            //如果前面一行是空字符串，返回前面一行的下标
            if index > 0 && (*originHosts)[index-1] == "" {
                start = index
            } else {
                start = index + 1
            }
        }
        if strings.Contains(line, homeHostsEndLine) {
            //如果后面一行是空字符串，返回前面一行的下标
            if index < len(*originHosts)-1 && (*originHosts)[index+1] == "" {
                end = index + 2
            } else {
                end = index + 1
            }
        }
    }
    if start > 0 && end > 0 {
        return true, start, end
    }
    return false, 0, 0
}

func getDefaultConfigFilePath() string {
    var homeDir, err = os.UserHomeDir()
    if err != nil {
        return ""
    }
    return homeDir + "/.HomeHosts/config.yaml"
}

func installLaunchAgent() {
    plistTemplate := `<plist version="1.0">
<dict>
    <key>KeepAlive</key>
    <true/>
    <key>Label</key>
    <string>com.jerehu.HomeHosts</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/home-hosts</string>
        <string>-f</string>
        <string>%d</string>
        <string>-c</string>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <false/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardErrorPath</key>
    <string>/var/log/home-hosts.err.log</string>
    <key>StandardOutPath</key>
    <string>/var/log/home-hosts.out.log</string>
</dict>
</plist>`
    plistContent := fmt.Sprintf(plistTemplate, *every, *configFilePath)

    // 将 plist 文件写入 /Library/LaunchDaemons/
    plistFile := os.ExpandEnv("/Library/LaunchDaemons/com.jerehu.HomeHosts.plist")
    os.WriteFile(plistFile, []byte(plistContent), 0644)
    exec.Command("launchctl", "load", plistFile).Run()
    fmt.Println("Launch agent installed.")
}

func uninstallLaunchAgent() {
    plistFile := os.ExpandEnv("/Library/LaunchDaemons/com.jerehu.HomeHosts.plist")
    exec.Command("launchctl", "unload", plistFile).Run()
    err := os.Remove(plistFile)
    if err != nil {
        fmt.Println(err.Error())
        return
    }
    fmt.Println("Launch agent uninstalled.")
}
