package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/alwayswangzi/sir"
)

type tmpFile struct {
	ID         string `json:"id"`
	FileName   string `json:"filename"`
	Size       int64  `json:"size"`
	CreatedAt  int64  `json:"created_at"`
	ExpireTime int64  `json:"expired_time"`
}

const (
	maxFileSize = 100 * 1000 * 1000
	tmpFileDir  = "tmp/"
)

var (
	tmpFileMap map[string]tmpFile
	useLetters = []rune("abcdefghjkmnpqrstuwxyz0123456789")
)

func main() {
	addShutdownHook(func() {
		err := saveTmpFileMap(tmpFileMap)
		if err != nil {
			panic(err)
		}
	})

	var err error
	tmpFileMap, err = loadTmpFileMap()
	if err != nil {
		panic(err)
	}

	cleanExpiredFiles(tmpFileMap, tmpFileDir)

	s := sir.New()

	s.Template("/index/", "index.html")

	s.Handler("/upload", uploadFile)
	s.Handler("/download", downloadFile)

	s.ListenAndServe(":8082")
}

func addShutdownHook(hook func()) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-c
		hook()
		os.Exit(1)
	}()
}

func loadTmpFileMap() (map[string]tmpFile, error) {
	bytes, err := ioutil.ReadFile("tmp_file_map.json")
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]tmpFile), nil
		}
		return nil, fmt.Errorf("load tmp_file_map.json failed, %v", err)
	}
	var m map[string]tmpFile
	err = json.Unmarshal(bytes, &m)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func saveTmpFileMap(m map[string]tmpFile) error {
	bytes, err := json.MarshalIndent(m, "", "\t")
	if err != nil {
		return err
	}
	return ioutil.WriteFile("tmp_file_map.json", bytes, 0666)
}

func randomStr() string {
	rand.Seed(time.Now().Unix())
	str := make([]rune, 4)
	for i := 0; i < 4; i++ {
		str[i] = useLetters[rand.Intn(len(useLetters))]
	}
	return string(str)
}

func uploadFile(c *sir.Ctx) {
	filename, bytes, err := c.Upload(maxFileSize, nil)
	if err != nil {
		c.Fail(err)
		return
	}
	id := randomStr()
	// if id exist, random another one
	for _, ok := tmpFileMap[id]; ok; {
		id = randomStr()
	}

	tmpFile := tmpFile{
		ID:         id,
		FileName:   filename,
		Size:       int64(len(bytes)),
		CreatedAt:  time.Now().Unix(),
		ExpireTime: 3600,
	}
	if t := c.GetQuery().Get("expired_time"); t != "" {
		i, err := strconv.ParseInt(t, 10, 64)
		if err != nil {
			c.Fail(fmt.Errorf("invalid param expired_time, %v", err))
			return
		}
		tmpFile.ExpireTime = i
	}
	tmpFileMap[id] = tmpFile
	if err = ioutil.WriteFile(tmpFileDir+id, bytes, 0666); err != nil {
		c.Fail(err)
		return
	}

	go cleanExpiredFiles(tmpFileMap, tmpFileDir)

	c.Success(id)
}

func downloadFile(c *sir.Ctx) {
	id := c.GetQuery().Get("id")
	if id == "" {
		c.BadRequest()
		return
	}
	file, ok := tmpFileMap[id]
	if !ok {
		c.BadRequest()
		return
	}
	if file.ExpireTime >= 0 && file.ExpireTime+file.CreatedAt < time.Now().Unix() {
		cleanExpiredFiles(tmpFileMap, tmpFileDir)
		c.Fail()
		return
	}
	bytes, err := ioutil.ReadFile(tmpFileDir + id)
	if err != nil {
		c.Fail(err)
		return
	}
	if err = c.Download(file.FileName, bytes); err != nil {
		c.Fail(err)
		return
	}
}

func cleanExpiredFiles(m map[string]tmpFile, dir string) {
	for name, file := range m {
		if file.ExpireTime >= 0 && file.ExpireTime+file.CreatedAt < time.Now().Unix() {
			err := os.Remove(dir + name)
			if err == nil {
				delete(m, name)
				continue
			}
			fmt.Printf("delete file %s failed, %v\n", name, err)
		}
	}
}
