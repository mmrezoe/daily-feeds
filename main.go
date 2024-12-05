package main

import (
	"database/sql"
	"encoding/xml"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mmrezoe/glnassistant"
	"gopkg.in/yaml.v2"
)

type Config struct {
	BotToken               string      `yaml:"bot_token"`
	ChatID                 string      `yaml:"chat_id"`
	MessageThreadIDYoutube string      `yaml:"message_thread_id_youtube"`
	MessageThreadIDMedium  string      `yaml:"message_thread_id_medium"`
	MessageThreadIDBlog    string      `yaml:"message_thread_id_blog"`
	MediumTags             []string    `yaml:"medium_tags"`
	YoutubeChannels        []string    `yaml:"youtube_channels"`
	BlogPosts              []BlogPosts `yaml:"blog-posts"`
}
type BlogPosts struct {
	Name string `yaml:"name"`
	Kind string `yaml:"kind"`
}

type Rss struct {
	Channel struct {
		Item []struct {
			Link string `xml:"link"`
		} `xml:"item"`
	} `xml:"channel"`
}

type Feed struct {
	Entry []struct {
		ID string `xml:"id"`
	} `xml:"entry"`
}

func main() {
	var just bool
	flag.BoolVar(&just, "j", false, "just run(don't notify)")
	flag.Usage = func() {
		flag.PrintDefaults()
		fmt.Println("version: 0.0.1")
	}
	flag.Parse()

	config := openConfig()

	results := make(map[string]string)
	for _, v := range youtube(config.YoutubeChannels) {
		results[v] = "youtube"
	}
	for _, v := range medium(config.MediumTags) {
		results[v] = "medium"
	}
	for _, v := range blog(config.BlogPosts) {
		results[v] = "blog"
	}

	glnassistant.Stderr("count", "result -> "+fmt.Sprintf("%v", len(results)))

	db, err := sql.Open("sqlite3", "./links.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	sqlStmt := `CREATE TABLE IF NOT EXISTS links(
	            link TEXT PRIMARY KEY,
	            UNIQUE(link)
	            );`
	_, err = db.Exec(sqlStmt)
	if err != nil {
		log.Fatalf("%q: %s\n", err, sqlStmt)
		return
	}

	uniq := make(map[string]bool)

	rows, err := db.Query("SELECT * FROM links;")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var link string
		err = rows.Scan(&link)
		if err != nil {
			log.Fatal(err)
		}
		uniq[link] = true
	}

	glnassistant.Stderr("count", "database -> "+fmt.Sprintf("%v", len(uniq)))

	for k, v := range results {
		if _, ok := uniq[k]; ok {
			continue
		}

		insertStmt := `INSERT INTO links (link) VALUES (?)`
		_, err = db.Exec(insertStmt, k)
		if err != nil {
			log.Fatal(err)
		}

		if !just {
			if v == "youtube" {
				notify(config.BotToken, config.ChatID, config.MessageThreadIDYoutube, k)
			} else if v == "medium" {
				notify(config.BotToken, config.ChatID, config.MessageThreadIDMedium, k)
			} else if v == "blog" {
				notify(config.BotToken, config.ChatID, config.MessageThreadIDBlog, k)
			}
		}
	}

	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}
}

func openConfig() Config {
	data, err := ioutil.ReadFile("config.yaml")
	if err != nil {
		log.Fatalf("Can't open file: %v\n", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		log.Fatalf("Can't Unmarshal: %v\n", err)
	}
	return config
}

func notify(bot_token, chat_id, message_thread_id, link string) {

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage?chat_id=%s&message_thread_id=%s&text=%s", bot_token, chat_id, message_thread_id, link)
	resp, err := http.Get(url)
	if err != nil {
		// log.Fatalf("Error making GET request: %v\n", err)
		glnassistant.Stderr("error", "Error request "+err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// log.Fatalf("GET request failed with status: %s", resp.Status)
		glnassistant.Stderr("error", "Can't Notify: "+link)
	}

	// body, err := ioutil.ReadAll(resp.Body)
	// if err != nil {
	// 	log.Fatalf("Error reading response body: %v\n", err)
	// }
	time.Sleep(time.Second * 2)
}

func request(url string) []byte {

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		glnassistant.Stderr("error", "create request: "+url)
		return nil
	}

	req.Header.Set("User-Agent", glnassistant.RandomUserAgent())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		glnassistant.Stderr("error", "Error making request: "+url)
		return nil
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		glnassistant.Stderr("error", "Can't read body: "+url)
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		glnassistant.Stderr("error", "Status: "+resp.Status+" "+url)
		return nil
	}

	glnassistant.Stderr("info", "-> "+url)
	time.Sleep(time.Second * 2)
	return body
}

func youtube(list []string) []string {
	var results []string
	for _, l := range list {
		res := request("https://www.youtube.com/feeds/videos.xml?channel_id=" + l)
		if res != nil {

			var response Feed
			err := xml.Unmarshal(res, &response)
			if err != nil {
				glnassistant.Stderr("error", "Unmarshal response: "+err.Error())
			}

			for _, r := range response.Entry {
				results = append(results, "https://www.youtube.com/watch?v="+strings.ReplaceAll(r.ID, "yt:video:", ""))
			}
		}
	}

	return results
}

func medium(tags []string) []string {
	var results []string

	for _, t := range tags {
		res := request("https://medium.com/feed/tag/" + t)
		if res != nil {
			var response Rss
			err := xml.Unmarshal(res, &response)
			if err != nil {
				glnassistant.Stderr("error", "Unmarshal response: "+err.Error())
			}

			for _, r := range response.Channel.Item {

				results = append(results, strings.Split(r.Link, "?")[0])
			}
		}
	}

	return results
}

func blog(list []BlogPosts) []string {
	var results []string

	for _, l := range list {
		res := request(l.Name)
		if res != nil {
			if l.Kind == "rss" {
				var response Rss
				err := xml.Unmarshal(res, &response)
				if err != nil {
					glnassistant.Stderr("error", "Unmarshal response: "+err.Error())
				}

				for _, r := range response.Channel.Item {
					results = append(results, r.Link)
				}

			} else {
				var response Feed
				err := xml.Unmarshal(res, &response)
				if err != nil {
					glnassistant.Stderr("error", "Unmarshal response: "+err.Error())
				}

				for _, r := range response.Entry {
					results = append(results, r.ID)
				}
			}
		}
	}

	return results
}
