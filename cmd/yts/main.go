package main

import (
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/gocolly/colly"
	"github.com/gocolly/colly/queue"
	commonLogger "github.com/hibare/GoCommon/v2/pkg/logger"
	"github.com/hibare/go-yts/internal/config"
	"github.com/hibare/go-yts/internal/history"
	"github.com/hibare/go-yts/internal/notifiers"
	"github.com/rs/zerolog/log"
)

func ConstructURL(baseUrl *url.URL, refUrl string) (string, error) {
	ref, err := url.Parse(refUrl)
	if err != nil {
		return "", err
	}

	u := baseUrl.ResolveReference(ref)

	return u.String(), nil
}

func ticker() {
	log.Info().Msg("[Start] Scraper task")

	movies := history.Movies{}
	urls := []string{"https://yts.mx/", "https://yts.autos/", "https://yts.rs/", "https://yts.lt/", "https://yts.do/"}

	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 6.1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2228.0 Safari/537.36"),
		colly.IgnoreRobotsTxt(),
	)

	c.WithTransport(&http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   config.Current.HTTPConfig.RequestTimeout,
			DualStack: true,
		}).DialContext,
	})

	c.SetRequestTimeout(config.Current.HTTPConfig.RequestTimeout)

	c.OnHTML("#popular-downloads", func(e *colly.HTMLElement) {
		temp := history.Movie{}
		e.ForEach("div .browse-movie-wrap", func(_ int, el *colly.HTMLElement) {
			var err error
			temp.Link = el.ChildAttr(".browse-movie-link", "href")
			temp.TimeStamp = time.Now()
			temp.Title = el.ChildText(".browse-movie-title")
			temp.Year = el.ChildText(".browse-movie-year")
			temp.CoverImage = el.ChildAttr("img", "src")

			temp.Link, err = ConstructURL(e.Request.URL, temp.Link)
			if err != nil {
				log.Error().Err(err)
				return
			}

			temp.CoverImage, err = ConstructURL(e.Request.URL, temp.CoverImage)
			if err != nil {
				log.Error().Err(err)
				return
			}

			movies[temp.Title] = temp
		})
	})

	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("Referrer", "https://www.google.com/")
		log.Info().Msgf("Visiting URL: %s", r.URL.String())
	})

	c.OnScraped(func(r *colly.Response) {
		log.Info().Msgf("Finished URL: %s", r.Request.URL.String())
	})

	c.OnResponse(func(r *colly.Response) {
		log.Info().Msgf("visited URL: %s Status Code: %d", r.Request.URL.String(), r.StatusCode)
	})

	c.OnError(func(r *colly.Response, err error) {
		log.Error().Err(err).Msgf("Failed to load URL: %s", r.Request.URL.String())
	})

	q, _ := queue.New(
		2, &queue.InMemoryQueueStorage{MaxSize: 100},
	)

	for _, url := range urls {
		q.AddURL(url)
	}

	q.Run(c)

	log.Info().Msgf("Scraped %d movies", len(movies))

	h := history.ReadHistory(config.Current.StorageConfig.DataDir, config.Current.StorageConfig.HistoryFile)
	diff := history.DiffHistory(movies, h)
	history.WriteHistory(diff, h, config.Current.StorageConfig.DataDir, config.Current.StorageConfig.HistoryFile)
	log.Info().Msgf("Found %d new movies", len(diff))

	notifiers.Notify(diff)

	log.Info().Msg("[End] Scraper task")
}

func main() {
	commonLogger.InitLogger()
	config.LoadConfig()
	log.Info().Msgf("Cron %s", config.Current.Schedule)
	log.Info().Msgf("Request Timeout %v", config.Current.HTTPConfig.RequestTimeout)
	log.Info().Msgf("Data directory %s", config.Current.StorageConfig.DataDir)
	log.Info().Msgf("History file %s", config.Current.StorageConfig.HistoryFile)
	log.Info().Msg("Starting scheduler")

	s := gocron.NewScheduler(time.UTC)
	s.Cron(config.Current.Schedule).Do(ticker)
	s.StartBlocking()
}
