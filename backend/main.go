package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	mgo "gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

const (
	MONGO_DB         = "test"
	MONGO_COLLECTION = "movies"
)

var db *mgo.Database

type Movie struct {
	ID          bson.ObjectId `bson:"_id" json:"id"`
	Title       string        `json:"title"`
	Cover       string        `json:"cover"`
	Description string        `json:"description"`
	UserScore   float64       `json:"userscore"`
}

func downloadFromS3(cfg aws.Config, key string) (string, error) {
	s3Client := s3.New(cfg)
	req := s3Client.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(os.Getenv("S3_BUCKET")),
		Key:    aws.String(key),
	})
	res, err := req.Send()
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	buf := bytes.NewBuffer(nil)
	if _, err := io.Copy(buf, res.Body); err != nil {
		return "", err
	}

	return string(buf.Bytes()), nil
}

func parseHTML(content string) (Movie, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	if err != nil {
		return Movie{}, err
	}

	title := doc.Find(".title span a").Text()
	percent, _ := doc.Find(".user_score_chart").Attr("data-percent")
	userScore, _ := strconv.ParseFloat(percent, 64)
	description := doc.Find(".overview p").Text()
	img, _ := doc.Find("div.poster div.image_content img").Attr("srcset")
	cover := strings.Split(img, " 1x")[0]

	return Movie{
		ID:          bson.NewObjectId(),
		Title:       title,
		Cover:       cover,
		Description: description,
		UserScore:   userScore,
	}, nil
}

func init() {
	log.Println("Connecting to MongoDB server")
	session, err := mgo.Dial(os.Getenv("MONGO_URI"))
	if err != nil {
		log.Fatal(err)
	}
	db = session.DB(MONGO_DB)

	log.Println("Downloading files from S3 bucket")
	cfg, err := external.LoadDefaultAWSConfig()
	if err != nil {
		log.Fatal(err)
	}

	s3Client := s3.New(cfg)
	req := s3Client.ListObjectsRequest(&s3.ListObjectsInput{
		Bucket: aws.String(os.Getenv("S3_BUCKET")),
	})
	res, err := req.Send()
	if err != nil {
		log.Fatal(err)
	}

	for _, object := range res.Contents {
		log.Println("Processing file:", *object.Key)
		data, err := downloadFromS3(cfg, *object.Key)
		if err != nil {
			log.Fatal(err)
		}
		movie, err := parseHTML(data)
		if err != nil {
			log.Fatal(err)
		}
		log.Println(movie)

		if err = db.C(MONGO_COLLECTION).Insert(&movie); err != nil {
			log.Fatal(err)
		}
		log.Println("Movie has been inserted")
	}
}

func GetMovies(w http.ResponseWriter, r *http.Request) {
	movies := make([]Movie, 0)
	db.C(MONGO_COLLECTION).Find(&bson.M{}).All(&movies)
	json.NewEncoder(w).Encode(&movies)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/movies", GetMovies)
	http.ListenAndServe(":"+port, nil)
}
