package main

import (
	"database/sql"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
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
			Link  string `xml:"link"`
			Title string `xml:"title"`
		} `xml:"item"`
	} `xml:"channel"`
}

type Feed struct {
	Entry []struct {
		ID    string `xml:"id"`
		Title string `xml:"title"`
	} `xml:"entry"`
}

type Link struct {
	link  string
	title string
}

func main() {
	var just bool
	flag.BoolVar(&just, "j", false, "just run(don't notify)")
	flag.Usage = func() {
		flag.PrintDefaults()
		fmt.Println("version: 0.0.3")
	}
	flag.Parse()

	config := openConfig()

	results := make(map[Link]string)
	for _, v := range youtube(config.YoutubeChannels) {
		results[v] = "youtube"
	}
	for _, v := range medium(config.MediumTags) {
		results[v] = "medium"
	}
	for _, v := range blog(config.BlogPosts) {
		results[v] = "blog"
	}

	glnassistant.Stdout("count", "result -> "+fmt.Sprintf("%v", len(results)))

	db, err := sql.Open("sqlite3", "./links.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	sqlStmt := `CREATE TABLE IF NOT EXISTS links(
	            link TEXT PRIMARY KEY,
				title TEXT NOT NULL,
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
		var title string
		err = rows.Scan(&link, &title)
		if err != nil {
			log.Fatal(err)
		}
		uniq[link] = true
	}

	glnassistant.Stdout("count", "database -> "+fmt.Sprintf("%v", len(uniq)))

	for k, v := range results {
		if _, ok := uniq[k.link]; ok {
			continue
		}

		insertStmt := `INSERT INTO links (link, title) VALUES (?, ?)`
		_, err = db.Exec(insertStmt, k.link, k.title)
		if err != nil {
			log.Fatal(err)
		}

		l := fmt.Sprintf("[%s](%s)", k.title, k.link)
		glnassistant.Stdout("notify", k.title)

		if !just {
			if v == "youtube" {
				notify(config.BotToken, config.ChatID, config.MessageThreadIDYoutube, l)
			} else if v == "medium" {
				notify(config.BotToken, config.ChatID, config.MessageThreadIDMedium, l)
			} else if v == "blog" {
				notify(config.BotToken, config.ChatID, config.MessageThreadIDBlog, l)
			}
		}
	}

	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}
}

func openConfig() Config {
	data, err := os.ReadFile("config.yaml")
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

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage?parse_mode=markdown&chat_id=%s&message_thread_id=%s&text=%s", bot_token, chat_id, message_thread_id, link)
	resp, err := http.Get(url)
	if err != nil {
		glnassistant.Stderr("Error request " + err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		glnassistant.Stderr("Can't Notify: " + link)
		return
	}

	time.Sleep(time.Second * 3)
}

func request(url string) []byte {

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		glnassistant.Stderr("create request: " + url)
		return nil
	}

	req.Header.Set("User-Agent", glnassistant.RandomUserAgent())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		glnassistant.Stderr("Error making request: " + url)
		return nil
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		glnassistant.Stderr("Can't read body: " + url)
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		glnassistant.Stderr("Status: " + resp.Status + " " + url)
		return nil
	}

	glnassistant.Stdout("request", "-> "+url)
	time.Sleep(time.Second * 2)
	return body
}

func youtube(list []string) []Link {
	var results []Link
	for _, l := range list {
		res := request("https://www.youtube.com/feeds/videos.xml?channel_id=" + l)
		if res != nil {

			var response Feed
			err := xml.Unmarshal(res, &response)
			if err != nil {
				glnassistant.Stderr("Unmarshal response: " + err.Error())
			}

			for _, r := range response.Entry {
				results = append(results, Link{
					link:  "https://www.youtube.com/watch?v=" + strings.ReplaceAll(r.ID, "yt:video:", ""),
					title: r.Title,
				})
			}
		}
	}

	return results
}

func medium(tags []string) []Link {
	var results []Link

	for _, t := range tags {
		res := request("https://medium.com/feed/tag/" + t)
		if res != nil {
			var response Rss
			err := xml.Unmarshal(res, &response)
			if err != nil {
				glnassistant.Stderr("Unmarshal response: " + err.Error())
			}

			for _, r := range response.Channel.Item {
				results = append(results, Link{
					link:  strings.Split(r.Link, "?")[0],
					title: r.Title,
				})
			}
		}
	}

	return results
}

func blog(list []BlogPosts) []Link {
	var results []Link

	for _, l := range list {
		res := request(l.Name)
		if res != nil {
			if l.Kind == "rss" {
				var response Rss
				err := xml.Unmarshal(res, &response)
				if err != nil {
					glnassistant.Stderr("Unmarshal response: " + err.Error())
				}

				for _, r := range response.Channel.Item {
					results = append(results, Link{
						link:  r.Link,
						title: r.Title,
					})
				}

			} else {
				var response Feed
				err := xml.Unmarshal(res, &response)
				if err != nil {
					glnassistant.Stderr("Unmarshal response: " + err.Error())
				}

				for _, r := range response.Entry {
					results = append(results, Link{
						link:  r.ID,
						title: r.Title,
					})
				}
			}
		}
	}

	return results
}
