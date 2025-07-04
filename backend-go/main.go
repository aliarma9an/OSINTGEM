package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gocolly/colly/v2"
)

// Veritabanındaki 'sources' tablosunun yapısı
type Source struct {
	ID      int
	Name    string
	URL     string
	Type    string
	RssFeed sql.NullString
}

// Kazınan veriyi tutacak basit bir struct
type ScrapedData struct {
	Title   string
	Content string
}

// scrapeSourceURL, verilen bir URL'e giderek içeriği kazır.
func scrapeSourceURL(url string) (ScrapedData, error) {
	log.Printf("Kazıma işlemi başlıyor: %s", url)
	var data ScrapedData

	// Yeni bir Colly collector (toplayıcı) oluştur.
	// AllowedDomains, scraper'ın sadece belirtilen domain'de kalmasını sağlar,
	// dışarıya giden linklere tıklamasını engeller.
	c := colly.NewCollector(
	// colly.AllowedDomains("aa.com.tr", "www.trthaber.com"), // İstersen domain'leri sınırlayabilirsin
	)

	// Olay 1: Bir H1 elementi bulunduğunda...
	// Çoğu haber sitesinde ana başlık <h1> etiketi içindedir.
	c.OnHTML("h1", func(e *colly.HTMLElement) {
		data.Title = e.Text
		log.Printf("Başlık bulundu: %s", e.Text)
	})

	// Olay 2: Bir makale içeriği elementi bulunduğunda...
	// Bu seçici (selector) en çok değişecek kısımdır. Hedef sitenin HTML yapısına
	// göre ayarlanmalıdır. Örnek olarak yaygın kullanılan class isimleri verilmiştir.
	// Örn: <div class="story-body"> veya <div class="article-content"> içindeki paragraflar (<p>)
	c.OnHTML("div.story-body p, div.article-content p, article p", func(e *colly.HTMLElement) {
		// Her paragrafı içeriğe ekle
		data.Content += e.Text + "\n"
	})

	// Olay 3: Bir hata oluşursa...
	c.OnError(func(r *colly.Response, err error) {
		log.Printf("İstek sırasında hata oluştu: %s -> %v", r.Request.URL, err)
	})

	// Olay 4: Kazıma işlemi bittiğinde...
	c.OnScraped(func(r *colly.Response) {
		log.Printf("Kazıma işlemi bitti: %s", r.Request.URL)
	})

	// Collector'a hedef URL'i ziyaret etmesini söyle
	err := c.Visit(url)
	if err != nil {
		return data, fmt.Errorf("site ziyaret edilemedi: %w", err)
	}

	// Toplanan veriyi geri döndür
	return data, nil
}

// getApprovedSources, veritabanından onaylanmış kaynakları çeker.
func getApprovedSources(db *sql.DB) ([]Source, error) {
	query := `SELECT id, name, url, type, rss_feed FROM sources WHERE status = 'approved'`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []Source
	for rows.Next() {
		var s Source
		if err := rows.Scan(&s.ID, &s.Name, &s.URL, &s.Type, &s.RssFeed); err != nil {
			continue
		}
		sources = append(sources, s)
	}
	return sources, nil
}

func main() {
	log.Println("Uygulama başlatılıyor...")
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"))
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("Veritabanı sürücüsü başlatılamadı: %v", err)
	}
	defer db.Close()
	if err = db.Ping(); err != nil {
		log.Fatalf("Veritabanına bağlanılamadı: %v", err)
	}
	log.Println("Veritabanına başarıyla bağlanıldı!")

	for {
		log.Println("Onaylanmış kaynaklar veritabanından çekiliyor...")
		approvedSources, err := getApprovedSources(db)
		if err != nil {
			log.Printf("Kaynaklar çekilirken hata oluştu: %v", err)
			time.Sleep(1 * time.Minute)
			continue
		}
		log.Printf("=> %d adet onaylanmış kaynak bulundu.", len(approvedSources))

		for _, source := range approvedSources {
			log.Printf("-> Görev Alındı: [%s] - %s", source.Type, source.Name)

			// YENİ EKLENEN KAZIMA ADIMI
			scrapedData, err := scrapeSourceURL(source.URL)
			if err != nil {
				log.Printf("Kaynak kazınırken hata oluştu (%s): %v", source.Name, err)
				continue // Bir kaynakta hata olursa diğerine geç
			}

			// Başarılı kazıma sonucunu logla
			if scrapedData.Title != "" {
				log.Printf("  \\_ BAŞLIK: %s", scrapedData.Title)
			} else {
				log.Println("  \\_ Bu sayfada başlık bulunamadı.")
			}

			// SONRAKİ ADIM: Bu 'scrapedData' verisini işlenmek üzere
			// Python servisine bir mesaj kuyruğu (RabbitMQ, Kafka vb.) üzerinden göndereceğiz
			// veya doğrudan Elasticsearch'e ham olarak kaydedeceğiz.
		}

		log.Println("Tüm kaynaklar için görevler tamamlandı. 5 dakika sonra tekrar kontrol edilecek.")
		time.Sleep(5 * time.Minute)
	}
}
