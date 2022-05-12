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
	ID          string `json:"id"`
	FileName    string `json:"filename"`
	Size        int64  `json:"size"`
	CreatedAt   int64  `json:"created_at"`
	ExpiredTime int64  `json:"expired_time"`
	Text        string `json:"text"`
}

const (
	maxFileSize        = 100 * 1000 * 1000
	tmpFileDir         = "tmp/"
	defaultExpiredTime = 2 * 60 * 60
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

	s.Template("/anyShare/index/", "index.html")

	s.Handler("/anyShare/upload", uploadFile)
	s.Handler("/anyShare/download", downloadFile)
	s.Handler("/anyShare/text", shareText)

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

	id := getRandomID(tmpFileMap)
	tmpFileMap[id] = tmpFile{
		ID:          id,
		FileName:    filename,
		Size:        int64(len(bytes)),
		CreatedAt:   time.Now().Unix(),
		ExpiredTime: getExpiredTime(c),
	}

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
	if isExpired(&file) {
		cleanExpiredFiles(tmpFileMap, tmpFileDir)
		c.Fail()
		return
	}

	if file.Text != "" {
		c.Success(file.Text)
	} else {
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
}

func isExpired(file *tmpFile) bool {
	return file.ExpiredTime >= 0 && file.ExpiredTime+file.CreatedAt < time.Now().Unix()
}

func cleanExpiredFiles(m map[string]tmpFile, dir string) {
	for name, file := range m {
		if isExpired(&file) {
			err := os.Remove(dir + name)
			if err == nil {
				delete(m, name)
				continue
			}
			fmt.Printf("delete file %s failed, %v\n", name, err)
		}
	}
}

func getRandomID(m map[string]tmpFile) string {
	id := randomStr()
	for _, ok := tmpFileMap[id]; ok; {
		id = randomStr()
	}
	return id
}

func shareText(c *sir.Ctx) {
	text := c.GetQuery().Get("text")
	if text == "" {
		c.Fail(fmt.Errorf("invalid param text"))
		return
	}

	id := getRandomID(tmpFileMap)
	tmpFileMap[id] = tmpFile{
		ID:          id,
		FileName:    "",
		Size:        int64(len(text)),
		CreatedAt:   time.Now().Unix(),
		ExpiredTime: getExpiredTime(c),
		Text:        text,
	}

	go cleanExpiredFiles(tmpFileMap, tmpFileDir)

	c.Success(id)
}

func getExpiredTime(c *sir.Ctx) int64 {
	if t := c.GetQuery().Get("expired_time"); t != "" {
		i, err := strconv.ParseInt(t, 10, 64)
		if err != nil {
			sir.LogError(fmt.Errorf("invalid param expired_time, %v", err))
			return 0
		}
		if i <= 0 {
			sir.LogError(fmt.Errorf("invalid expired_time=%d", i))
			return 0
		}
		return i
	}
	return defaultExpiredTime
}
