// Copyright 2019 ETH Zurich
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package lib

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	log "github.com/inconshreveable/log15"
	. "github.com/netsec-ethz/scion-apps/webapp/util"
	"github.com/scionproto/scion/go/lib/sciond"
)

// default params for localhost testing
var listenAddrDef = "127.0.0.1"
var listenPortDef = 8000
var cliPortDef = 30001
var serPortDef = 30100
var serDefAddr = "127.0.0.2"

var cfgFileCliUser = "config/clients_user.json"
var cfgFileSerUser = "config/servers_user.json"
var cfgFileCliDef = "config/clients_default.json"
var cfgFileSerDef = "config/servers_default.json"
var cfgFileSerIA = "config/servers_iaToSd.json"

var topologyFile = "topology.json"
var sdFile = "sd.toml"

// command argument constants
var CMD_ADR = "a"
var CMD_PRT = "p"
var CMD_ART = "sabin"
var CMD_WEB = "srvroot"
var CMD_BRT = "r"
var CMD_SCN = "sroot"
var CMD_SCB = "sbin"
var CMD_SCG = "sgen"
var CMD_SCC = "sgenc"
var CMD_SCL = "slogs"

// appsRoot is the root location of scionlab apps.
var GOPATH = os.Getenv("GOPATH")

// scionRoot is the root location of the scion infrastructure.
var DEF_SCIONDIR = path.Join(GOPATH, "src/github.com/scionproto/scion")

// UserSetting holds the serialized structure for persistent user settings
type UserSetting struct {
	MyIA      string `json:"myIa"`
	SDAddress string `json:"sdAddress"`
}

// topology holds the IA from topology.json
type topology struct {
	IA string `json:"isd_as"`
}

// sdTomlConfig holds the information from sd.toml
type sdTomlConfig struct {
	SD sdInfo `toml:"sd"`
}

// sdInfo holds the Sciond infomation from the sd field in sd.toml
type sdInfo struct {
	Address string `toml:"address"`
}


type CmdOptions struct {
	Addr          string
	Port          int
	StaticRoot    string
	BrowseRoot    string
	AppsRoot      string
	ScionBin      string
	ScionGen      string
	ScionGenCache string
	ScionLogs     string
}

func (o *CmdOptions) AbsPathCmdOptions() {
	o.StaticRoot, _ = filepath.Abs(o.StaticRoot)
	o.BrowseRoot, _ = filepath.Abs(o.BrowseRoot)
	o.AppsRoot, _ = filepath.Abs(o.AppsRoot)
	o.ScionBin, _ = filepath.Abs(o.ScionBin)
	o.ScionGen, _ = filepath.Abs(o.ScionGen)
	o.ScionGenCache, _ = filepath.Abs(o.ScionGenCache)
	o.ScionLogs, _ = filepath.Abs(o.ScionLogs)
}

func isFlagUsed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// defaultAppsRoot returns the directory containing the webapp executable as
// the default base directory for the apps resources
func defaultAppsRoot() string {
	exec, err := os.Executable()
	if err != nil {
		return ""
	}
	return path.Dir(exec)
}

func defaultStaticRoot(appsRoot string) string {
	return path.Join(appsRoot, "../webapp/web")
}

func defaultBrowseRoot(staticRoot string) string {
	return path.Join(staticRoot, "data")
}

func defaultScionBin() string {
	return "/usr/bin"
}

func defaultScionGen() string {
	return "/etc/scion"
}

func defaultScionGenCache() string {
	return "/var/lib/scion"
}

// TODO (mmalesev) change this to the new logs dir (if it exists)
func defaultScionLogs() string {
	return "logs"
}

func ParseFlags() CmdOptions {
	addr := flag.String(CMD_ADR, listenAddrDef, "Address of server host.")
	port := flag.Int(CMD_PRT, listenPortDef, "Port of server host.")
	appsRoot := flag.String(CMD_ART, defaultAppsRoot(),
		"Path to execute the installed scionlab apps binaries")
	staticRoot := flag.String(CMD_WEB, defaultStaticRoot(*appsRoot),
		"Path to read/write web server files.")
	browseRoot := flag.String(CMD_BRT, defaultBrowseRoot(*staticRoot),
		"Root path to read/browse from, CAUTION: read-access granted from -a and -p.")
	scionBin := flag.String(CMD_SCB, defaultScionBin(),
		"Path to execute SCION bin directory of infrastructure tools")
	scionGen := flag.String(CMD_SCG, defaultScionGen(),
		"Path to read SCION gen directory of infrastructure config")
	scionGenCache := flag.String(CMD_SCC, defaultScionGenCache(),
		"Path to read SCION gen-cache directory of infrastructure run-time config")
	scionLogs := flag.String(CMD_SCL, defaultScionLogs(),
		"Path to read SCION logs directory of infrastructure logging")
	flag.Parse()
	// recompute root args to use the proper relative defaults if undefined
	if !isFlagUsed(CMD_WEB) {
		*staticRoot = defaultStaticRoot(*appsRoot)
	}
	if !isFlagUsed(CMD_BRT) {
		*browseRoot = defaultBrowseRoot(*staticRoot)
	}

	if !isFlagUsed(CMD_SCB) {
		*scionBin = defaultScionBin()
	}
	if !isFlagUsed(CMD_SCG) {
		*scionGen = defaultScionGen()
	}
	if !isFlagUsed(CMD_SCC) {
		*scionGenCache = defaultScionGenCache()
	}
	if !isFlagUsed(CMD_SCL) {
		*scionLogs = defaultScionLogs()
	}
	options := CmdOptions{*addr, *port, *staticRoot, *browseRoot, *appsRoot,
		*scionBin, *scionGen, *scionGenCache, *scionLogs}
	options.AbsPathCmdOptions()
	return options
}

// WriteUserSetting writes the settings to disk.
func WriteUserSetting(options *CmdOptions, settings *UserSetting) {
	cliUserFp := path.Join(options.StaticRoot, cfgFileCliUser)
	settingsJSON, _ := json.Marshal(settings)

	// writing myIA means we have to retrieve sciond's tcp address too
	// since sciond's address may be autognerated
	sd, err := LoadSciondConfig(options, settings.MyIA)
	CheckError(err)
	settings.SDAddress = sd

	log.Info("Updating...", "UserSetting", string(settingsJSON))
	err = ioutil.WriteFile(cliUserFp, settingsJSON, 0644)
	CheckError(err)
}

// ReadUserSetting reads the settings from disk.
func ReadUserSetting(options *CmdOptions) UserSetting {
	var settings UserSetting
	cliUserFp := path.Join(options.StaticRoot, cfgFileCliUser)

	// no problem when user settings not set yet
	raw, _ := ioutil.ReadFile(cliUserFp)
	log.Debug("ReadUserSetting from saved", "settings", string(raw))
	json.Unmarshal([]byte(raw), &settings)

	return settings
}

// ScanLocalSetting will load list of locally available IAs and their corresponding Scionds
func ScanLocalSetting(options *CmdOptions) map[string]string {
	iaToSd := make(map[string]string)
	var searchPath = options.ScionGen
	filepath.Walk(searchPath, func(path string, f os.FileInfo, _ error) error {
		if f != nil && f.Name() == topologyFile {
			sdPath := path[:len(path)-len(topologyFile)] + sdFile
			ia := getIAFromTopologyFile(path)
			sd := getSDFromSDTomlFile(sdPath)
			iaToSd[ia] = sd
		}
		return nil
	})
	return iaToSd
}

// WriteLocalSettings writes the ia to sciond mapping to disk
func WriteLocalSettings(options *CmdOptions, iaToSD map[string]string) {
	fp := path.Join(options.StaticRoot, cfgFileSerIA)
	settingsJSON, _ := json.Marshal(iaToSD)
	log.Info("Updating...", "IAs and Scionds", string(settingsJSON))
	err := ioutil.WriteFile(fp, settingsJSON, 0644)
	CheckError(err)
}

// ReadLocalSettings reads the ia to sciond mappings from disk
func ReadLocalSettings(options *CmdOptions) map[string]string {
	iaToSD := make(map[string]string)
	fp := path.Join(options.StaticRoot, cfgFileSerIA)

	raw, err := ioutil.ReadFile(fp)
	CheckError(err)
	log.Debug("ReadLocalSettings from saved", "settings", string(raw))
	json.Unmarshal([]byte(raw), &iaToSD)

	return iaToSD
}

// getSDFromSDTomlFile returns sciond address from sd.toml on the given path
func getSDFromSDTomlFile(path string) string {
	var config sdTomlConfig
	if _, err := toml.DecodeFile(path, &config); err == nil {
		return config.SD.Address
	}
	// if sd.toml is not present, read from the environment variable SCION_DAEMON_ADDRESS
	sd, ok := os.LookupEnv("SCION_DAEMON_ADDRESS")
	if !ok {
		sd = sciond.DefaultAPIAddress
	}
	return sd
}

// getIAFromTopologyFile returns IA from topology.json on the given path
func getIAFromTopologyFile(path string) string {
	var t topology
	raw, _ := ioutil.ReadFile(path)
	json.Unmarshal([]byte(raw), &t)
	return t.IA
}

// StringInSlice can check a slice for a unique string
func StringInSlice(arr []string, i string) bool {
	for _, v := range arr {
		if v == i {
			return true
		}
	}
	return false
}

// Makes interfaces sortable, by preferred name
type byPrefInterface []net.Interface

func isInterfaceEn(c net.Interface) bool {
	return strings.HasPrefix(c.Name, "en")
}

func isInterfaceLocal(c net.Interface) bool {
	return strings.HasPrefix(c.Name, "lo")
}

func (c byPrefInterface) Len() int {
	return len(c)
}

func (c byPrefInterface) Swap(i, j int) {
	c[i], c[j] = c[j], c[i]
}

func (c byPrefInterface) Less(i, j int) bool {
	// sort "en*" interfaces first, then "lo", then alphabetically
	if isInterfaceEn(c[i]) && !isInterfaceEn(c[j]) {
		return true
	}
	if !isInterfaceEn(c[i]) && isInterfaceEn(c[j]) {
		return false
	}
	if isInterfaceLocal(c[i]) && !isInterfaceLocal(c[j]) {
		return true
	}
	if !isInterfaceLocal(c[i]) && isInterfaceLocal(c[j]) {
		return false
	}
	return c[i].Name < c[j].Name
}

// GenServerNodeDefaults creates server defaults for localhost testing
func GenServerNodeDefaults(options *CmdOptions, localIAs []string) {
	// reverse sort so that the default server will oppose the default client
	sort.Sort(sort.Reverse(sort.StringSlice(localIAs)))

	serFp := path.Join(options.StaticRoot, cfgFileSerUser)
	jsonBuf := []byte(`{ "all": [`)
	for i := 0; i < len(localIAs); i++ {
		// use all localhost endpoints as possible servers for bwtester as least
		ia := strings.Replace(localIAs[i], "_", ":", -1)
		json := []byte(`{"name":"lo ` + ia + `","isdas":"` + ia +
			`", "addr":"` + serDefAddr + `","port":` + strconv.Itoa(serPortDef) +
			`}`)
		jsonBuf = append(jsonBuf, json...)
		if i < (len(localIAs) - 1) {
			jsonBuf = append(jsonBuf, []byte(`,`)...)
		}
	}
	jsonBuf = append(jsonBuf, []byte(`] }`)...)
	err := ioutil.WriteFile(serFp, jsonBuf, 0644)
	CheckError(err)
}

// GenClientNodeDefaults queries network interfaces and writes local client
// SCION addresses as json
func GenClientNodeDefaults(options *CmdOptions, cisdas string) {
	cliFp := path.Join(options.StaticRoot, cfgFileCliDef)

	// find interface addresses
	jsonBuf := []byte(`{ "all": [ `)
	ifaces, err := net.Interfaces()
	if CheckError(err) {
		return
	}
	sort.Sort(byPrefInterface(ifaces))
	idx := 0
	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if CheckError(err) {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				if ipnet.IP.To4() != nil {
					if idx > 0 {
						jsonBuf = append(jsonBuf, []byte(`, `)...)
					}
					cname := i.Name
					caddr := ipnet.IP.String()
					jsonInterface := []byte(`{"name":"` + cname + `", "isdas":"` +
						cisdas + `", "addr":"` + caddr + `","port":` +
						strconv.Itoa(cliPortDef) + `}`)
					jsonBuf = append(jsonBuf, jsonInterface...)
					idx++
				}
			}
		}
	}
	jsonBuf = append(jsonBuf, []byte(` ] }`)...)
	err = ioutil.WriteFile(cliFp, jsonBuf, 0644)
	CheckError(err)
}

// GetNodesHandler queries the local environment for user/default nodes.
func GetNodesHandler(w http.ResponseWriter, r *http.Request, options *CmdOptions) {
	r.ParseForm()
	nodes := r.PostFormValue("node_type")
	var fp string
	switch nodes {
	case "clients_default":
		fp = path.Join(options.StaticRoot, cfgFileCliDef)
	case "servers_default":
		fp = path.Join(options.StaticRoot, cfgFileSerDef)
	case "clients_user":
		fp = path.Join(options.StaticRoot, cfgFileCliUser)
	case "servers_user":
		fp = path.Join(options.StaticRoot, cfgFileSerUser)
	default:
		panic("Unhandled nodes type!")
	}
	raw, err := ioutil.ReadFile(fp)
	CheckError(err)
	fmt.Fprint(w, string(raw))
}
