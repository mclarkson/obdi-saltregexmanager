// Obdi - a REST interface and GUI for deploying software
// Copyright (C) 2014  Mark Clarkson
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"strings"
	//"regexp"
	"github.com/jinzhu/gorm"
	_ "github.com/mattn/go-sqlite3"
	"strconv"
	"time"
)

// ***************************************************************************
// SQLITE3 PRIVATE DB
// ***************************************************************************

type Enc struct {
	Id        int64
	SaltId    string // Name of the server
	Formula   string // Directory name
	StateFile string // Sls file name
	Dc        string // Data centre name
	Env       string // Environment name
}

type Regex struct {
	Id    int64
	Regex string // The regular expression
	Dc    string // Data centre name
	Env   string // Environment name
	Name  string // Short name for the regex, no spaces
	Desc  string // Description of the regex
}

type RegexSlsMap struct {
	Id        int64
	RegexId   int64  // Not null
	Formula   string // Not null
	StateFile string // Can be null
}

// --

var config *Config

type Config struct {
	Dbname   string
	Portlock *PortLock
	Port     int
}

func (c *Config) DBPath() string {

	return c.Dbname
}

func (c *Config) SetDBPath(path string) {

	c.Dbname = path
}

func NewConfig() {

	config = &Config{}
}

// --

type GormDB struct {
	db gorm.DB
}

func (gormInst *GormDB) InitDB() error {

	var err error
	dbname := config.DBPath()

	gormInst.db, err = gorm.Open("sqlite3", dbname+"enc.db")
	if err != nil {
		return ApiError{"Open " + dbname + " failed. " + err.Error()}
	}

	if err := gormInst.db.AutoMigrate(Enc{}).Error; err != nil {
		txt := fmt.Sprintf("AutoMigrate Enc table failed: %s", err)
		return ApiError{txt}
	}
	if err := gormInst.db.AutoMigrate(Regex{}).Error; err != nil {
		txt := fmt.Sprintf("AutoMigrate Regex table failed: %s", err)
		return ApiError{txt}
	}
	if err := gormInst.db.AutoMigrate(RegexSlsMap{}).Error; err != nil {
		txt := fmt.Sprintf("AutoMigrate RegexSlsMap table failed: %s", err)
		return ApiError{txt}
	}

	// Unique index is also a constraint, so are forced to be unique
	gormInst.db.Model(Enc{}).AddIndex("idx_enc_salt_id", "salt_id")

	return nil
}

func (gormInst *GormDB) DB() *gorm.DB {

	return &gormInst.db
}

func NewDB() (*GormDB, error) {

	gormInst := &GormDB{}
	if err := gormInst.InitDB(); err != nil {
		return gormInst, err
	}
	return gormInst, nil
}

// ***************************************************************************
// PORT LOCKING
// ***************************************************************************

// PortLock is a locker which locks by binding to a port on the loopback IPv4
// interface
type PortLock struct {
	hostport string
	ln       net.Listener
}

func NewPortLock(port int) *PortLock {

	// NewFLock creates new Flock-based lock (unlocked first)
	return &PortLock{hostport: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}
}

func (p *PortLock) Lock() {

	// Lock acquires the lock, blocking
	t := 50 * time.Millisecond
	for {
		if l, err := net.Listen("tcp", p.hostport); err == nil {
			p.ln = l // thanks to zhangpy
			return
		}
		//log.Printf("spinning lock on %s (%s)", p.hostport, err)
		time.Sleep(t)
		//t = time.Duration(
		//  math.Min( float64(time.Duration(float32(t) * 1.5)), 2000 ))
	}
}

func (p *PortLock) Unlock() {

	// Unlock releases the lock
	if p.ln != nil {
		p.ln.Close()
	}
}

// ***************************************************************************
// GO RPC PLUGIN
// ***************************************************************************

type PostedData struct {
	Classes []string
	RegexId int64
}

func Unlock() {

	config.Portlock.Unlock()
}

func Lock() {

	config.Portlock.Lock()
}

func (t *Plugin) GetRequest(args *Args, response *[]byte) error {

	// Return list of all regex_sls_maps for an environment

	// Check for required query string entries

	if len(args.QueryString["env_id"]) == 0 {
		ReturnError("'env_id' must be set", response)
		return nil
	}

	env_id_str := args.QueryString["env_id"][0]

	// Check if the user is allowed to access the environment
	var err error
	if _, err = t.GetAllowedEnv(args, env_id_str, response); err != nil {
		// GetAllowedEnv wrote the error
		return nil
	}

	// If we get this far then the user is allowed access to this env.

	// PluginDatabasePath is required to open our private db
	if len(args.PathParams["PluginDatabasePath"]) == 0 {
		ReturnError("Internal Error: 'PluginDatabasePath' must be set", response)
		return nil
	}

	config.SetDBPath(args.PathParams["PluginDatabasePath"])

	// Open/Create database
	var gormInst *GormDB
	if gormInst, err = NewDB(); err != nil {
		txt := "GormDB open error for '" + config.DBPath() + "enc.db'. " +
			err.Error()
		ReturnError(txt, response)
		return nil
	}

	// Get Regex formula's and state files from enc tables
	// Do we care who can get this information? I'm guessing 'no'.

	db := gormInst.DB() // shortcut

	// Search the regex_sls_maps table

	maps := []RegexSlsMap{}

	if len(args.QueryString["regex_id"]) == 0 {
		// No regex_id was sent. Show all maps
		Lock()
		if err := db.Find(&maps); err.Error != nil {
			if !err.RecordNotFound() {
				Unlock()
				ReturnError(err.Error.Error(), response)
				return nil
			}
		}
		Unlock()
	} else {
		// Search for a specific regex_id mapping
		regex_id := args.QueryString["regex_id"][0]
		Lock()
		if err := db.Find(&maps, "regex_id = ?", regex_id); err.Error != nil {
			if !err.RecordNotFound() {
				Unlock()
				ReturnError(err.Error.Error(), response)
				return nil
			}
		}
		Unlock()
	}

	// Output as JSON

	u := make([]map[string]interface{}, len(maps))
	for i := range maps {
		u[i] = make(map[string]interface{})
		u[i]["Id"] = maps[i].Id
		u[i]["RegexId"] = maps[i].RegexId
		u[i]["Formula"] = maps[i].Formula
		u[i]["StateFile"] = maps[i].StateFile
	}

	type JsonOut struct {
		Text string
	}

	TempJsonData, err := json.Marshal(u)
	if err != nil {
		ReturnError("Marshal error: "+err.Error(), response)
		return nil
	}
	reply := Reply{0, string(TempJsonData), SUCCESS, ""}
	jsondata, err := json.Marshal(reply)

	if err != nil {
		ReturnError("Marshal error: "+err.Error(), response)
		return nil
	}

	*response = jsondata

	return nil
}

func (t *Plugin) PostRequest(args *Args, response *[]byte) error {

	// Needed if the salt version has been changed
	if len(args.QueryString["env_id"]) == 0 {
		ReturnError("'env_id' must be set", response)
		return nil
	}

	env_id_str := args.QueryString["env_id"][0]

	// Check if the user is allowed to access the environment
	var err error
	if _, err = t.GetAllowedEnv(args, env_id_str, response); err != nil {
		// GetAllowedEnv wrote the error
		return nil
	}

	// PluginDatabasePath is required to open our private db
	if len(args.PathParams["PluginDatabasePath"]) == 0 {
		ReturnError("Internal Error: 'PluginDatabasePath' must be set", response)
		return nil
	}

	config.SetDBPath(args.PathParams["PluginDatabasePath"])

	// Open/Create database
	var gormInst *GormDB
	if gormInst, err = NewDB(); err != nil {
		txt := "GormDB open error for '" + config.DBPath() + "enc.db'. " +
			err.Error()
		ReturnError(txt, response)
		return nil
	}

	// Decode the post data into struct

	var postdata PostedData

	if err := json.Unmarshal(args.PostData, &postdata); err != nil {
		txt := fmt.Sprintf("Error decoding JSON ('%s')"+".", err.Error())
		ReturnError("Error decoding the POST data ("+
			fmt.Sprintf("%s", args.PostData)+"). "+txt, response)
		return nil
	}

	db := gormInst.DB() // shortcut

	// Remove all RegexSLSMap Classes (before adding)

	Lock()
	if err := db.Where("regex_id = ?", postdata.RegexId).Delete(RegexSlsMap{}); err.Error != nil {
		if !err.RecordNotFound() {
			Unlock()
			ReturnError(err.Error.Error(), response)
			return nil
		}
	}
	Unlock()

	// Add the ENC classes

	for i := range postdata.Classes {
		classes := strings.Split(postdata.Classes[i], ".")
		formula := ""
		statefile := ""
		switch len(classes) {
		case 0:
			continue
		case 1:
			formula = classes[0]
		case 2:
			formula = classes[0]
			statefile = classes[1]
		}
		regexmap := RegexSlsMap{
			Id:        0,
			Formula:   formula,
			StateFile: statefile,
			RegexId:   postdata.RegexId,
		}
		Lock()
		if err := db.Create(&regexmap); err.Error != nil {
			Unlock()
			ReturnError(err.Error.Error(), response)
			return nil
		}
		Unlock()
	}

	reply := Reply{0, "", SUCCESS, ""}
	jsondata, err := json.Marshal(reply)

	if err != nil {
		ReturnError("Marshal error: "+err.Error(), response)
		return nil
	}

	*response = jsondata

	return nil
}

func (t *Plugin) HandleRequest(args *Args, response *[]byte) error {

	// All plugins must have this.

	if len(args.QueryType) > 0 {
		switch args.QueryType {
		case "GET":
			t.GetRequest(args, response)
			return nil
		case "POST":
			t.PostRequest(args, response)
			return nil
		}
		ReturnError("Internal error: Invalid HTTP request type for this plugin "+
			args.QueryType, response)
		return nil
	} else {
		ReturnError("Internal error: HTTP request type was not set", response)
		return nil
	}
}

// ***************************************************************************
// ENTRY POINT
// ***************************************************************************

func main() {

	// Sets the global config var
	NewConfig()

	// Create a lock file to use for synchronisation
	config.Port = 49993
	config.Portlock = NewPortLock(config.Port)

	plugin := new(Plugin)
	rpc.Register(plugin)

	listener, err := net.Listen("tcp", ":"+os.Args[1])
	if err != nil {
		txt := fmt.Sprintf("Listen error. ", err)
		logit(txt)
	}

	if conn, err := listener.Accept(); err != nil {
		txt := fmt.Sprintf("Accept error. ", err)
		logit(txt)
	} else {
		rpc.ServeConn(conn)
	}
}

// vim:ts=2:sw=2
