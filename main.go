package main

import (
    "HomeHosts/config"
    "bytes"
    "flag"
    "fmt"
    "github.com/hashicorp/go-version"
    "github.com/kardianos/service"
    "gopkg.in/yaml.v3"
    "log"
    "os"
    "os/exec"
    "regexp"
    "runtime"
    "slices"
    "strconv"
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
        installService()
    case "uninstall":
        uninstallService()
    case "restart":
        restartService()
    case "modify":
        modifyHostsFile(&myConfig)
        return
    case "restore":
        restoreHostsFile()
    default:
        s := getService()
        status, _ := s.Status()
        if status != service.StatusUnknown {
            // 以服务方式运行
            s.Run()
        } else {
            run(&myConfig)
        }
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
    osVersion, _ := getMacVersion()
    fmt.Println("os version: " + osVersion)
    currentVersion, _ := version.NewVersion(osVersion)
    version15, _ := version.NewVersion("15.0.0")
    version145, _ := version.NewVersion("14.5.0")
    version144, _ := version.NewVersion("14.4.0")
    version1369, _ := version.NewVersion("13.6.9")
    var out []byte
    var err error
    if currentVersion.GreaterThanOrEqual(version15) {
        cmd := "system_profiler SPAirPortDataType | awk '/Current Network Information:/ { getline; print substr($0, 13, (length($0) - 13)); exit }'"
        out, err = exec.Command("bash", "-c", cmd).Output()
    } else if currentVersion.GreaterThanOrEqual(version145) {
        cmd := "/usr/sbin/networksetup -getairportnetwork en0 | /usr/bin/awk -F \": \" '{ print $2 }'"
        out, err = exec.Command("bash", "-c", cmd).Output()
    } else if currentVersion.GreaterThanOrEqual(version144) {
        cmd := "/usr/bin/wdutil info | /usr/bin/awk '/SSID/ { print $NF }' | head -n 1"
        out, err = exec.Command("bash", "-c", cmd).Output()
    } else if currentVersion.GreaterThanOrEqual(version1369) {
        cmd := "/System/Library/PrivateFrameworks/Apple80211.framework/Versions/A/Resources/airport --getinfo | /usr/bin/awk '/ SSID/{print $NF}'"
        out, err = exec.Command("bash", "-c", cmd).Output()
    } else {
        out = []byte("")
        err = fmt.Errorf("os version is rather low")
    }

    if err != nil {
        fmt.Println("Error checking Wi-Fi connection:", err)
        return "", err
    }
    return strings.TrimSpace(string(out)), nil
}

func getMacVersion() (string, error) {
    // 使用 sw_vers 命令来获取 macOS 版本信息
    cmd := exec.Command("sw_vers")
    var out bytes.Buffer
    cmd.Stdout = &out
    err := cmd.Run()
    if err != nil {
        return "", err
    }

    // 根据输出处理版本信息
    versionInfo := out.String()
    re := regexp.MustCompile(`ProductVersion:\s*(.+)`)
    matches := re.FindStringSubmatch(versionInfo)
    if len(matches) > 1 {
        return matches[1], nil
    }
    return "", fmt.Errorf("macOS version not found")
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

type program struct{}

func (p *program) Start(s service.Service) error {
    // Start should not block. Do the actual work async.
    go p.run()
    return nil
}
func (p *program) run() {
    run(&myConfig)
}
func (p *program) Stop(s service.Service) error {
    // Stop should not block. Return with a few seconds.
    return nil
}

func installService() {
    s := getService()

    status, err := s.Status()
    if err != nil && status == service.StatusUnknown {
        // 服务未知，创建服务
        if err = s.Install(); err == nil {
            s.Start()
            fmt.Println("安装 home-hosts 服务成功! ")
            if service.ChosenSystem().String() == "unix-systemv" {
                if _, err := exec.Command("/etc/init.d/home-hosts", "enable").Output(); err != nil {
                    log.Println(err)
                }
                if _, err := exec.Command("/etc/init.d/home-hosts", "start").Output(); err != nil {
                    log.Println(err)
                }
            }
            return
        }
        fmt.Println("安装 home-hosts 服务失败, 异常信息: %s", err)
    }

    if status != service.StatusUnknown {
        fmt.Println("home-hosts 服务已安装, 无需再次安装")
    }
}

func uninstallService() {
    s := getService()
    s.Stop()
    if service.ChosenSystem().String() == "unix-systemv" {
        if _, err := exec.Command("/etc/init.d/home-hosts", "stop").Output(); err != nil {
            log.Println(err)
        }
    }
    if err := s.Uninstall(); err == nil {
        fmt.Println("home-hosts 服务卸载成功")
    } else {
        fmt.Println("home-hosts 服务卸载失败, 异常信息: %s", err)
    }
}

func restartService() {
    s := getService()
    status, err := s.Status()
    if err == nil {
        if status == service.StatusRunning {
            if err = s.Restart(); err == nil {
                fmt.Println("重启 home-hosts 服务成功")
            }
        } else if status == service.StatusStopped {
            if err = s.Start(); err == nil {
                fmt.Println("启动 home-hosts 服务成功")
            }
        }
    } else {
        fmt.Println("home-hosts 服务未安装, 请先安装服务")
    }
}

func getService() service.Service {
    options := make(service.KeyValue)
    var depends []string

    // 确保服务等待网络就绪后再启动
    switch service.ChosenSystem().String() {
    case "unix-systemv":
        options["SysvScript"] = sysvScript
    case "windows-service":
        // 将 Windows 服务的启动类型设为自动(延迟启动)
        options["DelayedAutoStart"] = true
    default:
        // 向 Systemd 添加网络依赖
        depends = append(depends, "Requires=network.target",
            "After=network-online.target")
    }

    svcConfig := &service.Config{
        Name:         "home-hosts",
        DisplayName:  "home-hosts",
        Description:  "根据wifi名自动切换本地hosts",
        Arguments:    []string{"-c", *configFilePath, "-f", strconv.Itoa(*every)},
        Dependencies: depends,
        Option:       options,
    }

    prg := &program{}
    s, err := service.New(prg, svcConfig)
    if err != nil {
        log.Fatalln(err)
    }
    return s
}

const sysvScript = `#!/bin/sh /etc/rc.common
DESCRIPTION="{{.Description}}"
cmd="{{.Path}}{{range .Arguments}} {{.|cmd}}{{end}}"
name="home-hosts"
pid_file="/var/run/$name.pid"
stdout_log="/var/log/$name.log"
stderr_log="/var/log/$name.err"
START=99
get_pid() {
    cat "$pid_file"
}
is_running() {
    [ -f "$pid_file" ] && cat /proc/$(get_pid)/stat > /dev/null 2>&1
}
start() {
	if is_running; then
		echo "Already started"
	else
		echo "Starting $name"
		{{if .WorkingDirectory}}cd '{{.WorkingDirectory}}'{{end}}
		$cmd >> "$stdout_log" 2>> "$stderr_log" &
		echo $! > "$pid_file"
		if ! is_running; then
			echo "Unable to start, see $stdout_log and $stderr_log"
			exit 1
		fi
	fi
}
stop() {
	if is_running; then
		echo -n "Stopping $name.."
		kill $(get_pid)
		for i in $(seq 1 10)
		do
			if ! is_running; then
				break
			fi
			echo -n "."
			sleep 1
		done
		echo
		if is_running; then
			echo "Not stopped; may still be shutting down or shutdown may have failed"
			exit 1
		else
			echo "Stopped"
			if [ -f "$pid_file" ]; then
				rm "$pid_file"
			fi
		fi
	else
		echo "Not running"
	fi
}
restart() {
	stop
	if is_running; then
		echo "Unable to stop, will not attempt to start"
		exit 1
	fi
	start
}
`
