<img width="1394" height="125" alt="image" src="https://github.com/user-attachments/assets/d4134017-d05b-4feb-81be-8a4182a1094e" /># Device Registry

Bir organizasyona bağlı cihazları kaydeden, heartbeat'lerini takip eden ve heartbeat'i kesilen cihazları offline yapan küçük bir Go servisi.

Endpoint'ler:

- `POST /orgs/{orgID}/devices`
- `GET /orgs/{orgID}/devices`
- `GET /orgs/{orgID}/devices/{deviceID}`
- `DELETE /orgs/{orgID}/devices/{deviceID}`
- `POST /orgs/{orgID}/devices/{deviceID}/heartbeat`

## 1. Çalıştırma

```bash
docker compose up --build
```

Servis `localhost:8080` üzerinde açılır, migration'lar açılışta otomatik uygulanır.

```bash
curl -X POST localhost:8080/orgs/acme/devices -d '{"name":"build-server-01","platform":"linux"}'
curl localhost:8080/orgs/acme/devices
```

### Testler

Testleri çalıştırmak için Go 1.26 gerekir (Docker ile çalıştırmak için gerekmez).

```bash
go test -race ./...
```

Veritabanı gerektiren testler `TEST_DATABASE_URL` tanımlı değilse atlanır. Hepsini çalıştırmak için:

```bash
docker compose up -d db
TEST_DATABASE_URL="postgres://devices:devices@localhost:5432/devices?sslmode=disable" go test -race ./...
```

Bu testler `devices` tablosunu temizler.

### Konfigürasyon

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `DATABASE_URL` | (zorunlu) | Postgres bağlantı adresi |
| `HTTP_ADDR` | `:8080` | Dinlenen adres |
| `MAX_DEVICES_PER_ORG` | `5` | Org başına cihaz limiti |
| `HEARTBEAT_TIMEOUT` | `60s` | Bu süre heartbeat gelmezse cihaz offline olur |
| `WORKER_INTERVAL` | `10s` | Offline işaretleme işinin çalışma aralığı |
| `SHUTDOWN_TIMEOUT` | `10s` | Kapanışta süren isteklere tanınan süre |

Docker ile çalışırken değiştirmek için `docker-compose.yml` içinde `app` servisinin `environment` kısmına eklemek yeterli.

## 2. Teknoloji seçimi

**HTTP: standart kütüphane (net/http).**
Serviste 5 endpoint olduğu için ek paket eklemeye gerek görmedim, Go 1.22'den beri standart kütüphane metoda ve yol parametresine göre routing yapabiliyor.
Maliyeti: JSON okuma/yazma ve hata eşlemeyi kendim yazmam gerekti. Daha büyük bir serviste, çok endpoint ve ortak middleware (yetkilendirme, log gibi) olunca Gin veya Echo daha mantıklı olurdu.

**Veritabanı: pgx + düz SQL.**
Bu serviste en önemli nokta eşzamanlılık. Limit kontrolü ve insert'in aynı transaction'da olması, advisory lock ve atomik UPDATE gibi şeyler için SQL'e doğrudan hâkim olmak istedim.
Maliyeti: SQL string olduğu için derleyici kontrol etmiyor, bu yüzden gerçek veritabanına karşı testler yazdım. Satırları Go yapısına çevirmeyi ve Postgres hatalarını kendi hatalarıma çevirmeyi de elle yazdım.

**Migration: goose.**
Case şema değişikliklerinin migration olarak gelmesini istiyor. Goose düz SQL dosyalarıyla çalışıyor ve Go'da kullanması kolay.
Maliyeti: migration servis açılırken çalıştığı için birden fazla kopya aynı anda kalkarsa çakışabilir. Tek kopya çalıştığı için şimdilik kilit eklemedim.

## 3. Varsayımlar

PDF'te bilerek açık bırakılmış noktalar için soru sormak yerine makul varsayımlar yaptım:

**Org'lar için tablo yok.** Org'lar için herhangi bir tablo verilmemişti. Bu tabloyu oluşturmak seed ve ek kod eklemek gerektirdiği için kapsamı genişletecekti, o yüzden tablo oluşturmayıp sadece `org_id` kullandım.

**Yeni kaydedilen cihaz offline.** Yeni kaydedilen cihazların hangi statüyle kaydedileceği dökümanda belirtilmemişti. Son 60 saniyede heartbeat gönderen cihaz online olması gerektiği için, yeni kayıtların offline olması gerektiğini varsayarak geliştirdim. İlk heartbeat'te online oluyor.

**Heartbeat cihazı online yapar.** Heartbeat atan cihaz offline olsa bile tekrar online olur, çünkü heartbeat geldiyse cihaz çalışıyor demektir. Olmayan bir cihaz için heartbeat gelirse 404 döndürüyorum.

**Tam 60 saniye hâlâ online.** Kural "son 60 saniye içinde" dediği için, heartbeat'ten tam 60 saniye geçmiş cihazı online sayıyorum, 60 saniyeyi aşınca offline oluyor.

**İsimler büyük/küçük harfe duyarlı.** İsim benzersizliğini `UNIQUE (org_id, name)` ile sağladığım için ayrıca bir kural yazmadım. Postgres'in varsayılan metin karşılaştırması büyük/küçük harfe duyarlı olduğu için `Build-01` ile `build-01` farklı cihazlar sayılıyor.

**İsim ve platform alanları.** Dökümanda isim ve platform için herhangi bir liste olmadığı için bu alanları uydurmak istemedim, ikisi de serbest metin. Alanları baştan sondan kırpıp boş olmamasını ve uzunluk sınırını (isim 100, platform 50) kontrol ediyorum. Bu sınırları ben belirledim.

**Org ID herhangi bir metin.** Org'ların nasıl var olacağı bana bırakıldığı için org ID'ye biçim kuralı koymadım. Baştan sondan boşlukları kırpılmış, boş olmayan, NUL karakteri içermeyen ve en fazla 100 karakterlik her metin geçerli.

**Heartbeat zamanı uygulamanın saatinden geliyor.** Heartbeat zamanını veritabanının `now()` değerinden değil, uygulamanın saatinden alıyorum, çünkü testlerde saati kontrol edebilmek istedim. Bunun bedeli: servisin birden fazla kopyası farklı saatlerde çalışırsa heartbeat zamanları tutarsız olabilir.

## 4. Bir karar: org kilidi

Bir org en fazla 5 cihaz (varsayılan, ayarlanabilir) taşıyabilir. Aynı org'a aynı anda birden fazla kayıt isteği gelirse "kaç cihaz var" kontrolü ile insert arasında bir boşluk oluşuyor: hepsi sayıyı aynı anda okuyup "limit dolmadı" diyebilir ve limit aşılır. Bunu önlemek için kayıt işlemini tek bir transaction içinde yapıyorum ve en başta o org için bir advisory lock alıyorum (`pg_advisory_xact_lock`). Böylece aynı org'un istekleri sırayla çalışıyor, her istek öncekilerin eklediği cihazları görüyor ve limitin üzerindeki istek reddediliyor. Lock transaction bitince kendiliğinden bırakılıyor. Farklı org'lar farklı lock aldığı için birbirini beklemiyor. Bunu test ettim: bir denemede lock'u kaldırınca 20 eşzamanlı istekten 5 yerine 11 cihaz eklendi.

Değerlendirip seçmediğim alternatif: bir `orgs` tablosu açıp o org'un satırını `SELECT ... FOR UPDATE` ile kilitlemek. Bu daha doğal bir yöntem olurdu ve olmayan org için 404 vermek gibi şeyleri de getirirdi. Ama org yönetimi kapsam dışı olduğu için tablo, seed ve ek kod eklemek istemedim, o yüzden advisory lock seçtim.

Lock anahtarı org ID'nin hash'i olduğu için iki farklı org'un hash'i çakışırsa birbirini gereksiz yere bekleyebilir. Sonuç yine doğru olur ama o istekler biraz yavaşlar. Ayrıca kilit bir satıra değil bir sayıya bağlı olduğu için veritabanına bakarak ne kilitlendiğini anlamak daha zor.

## 5. Eksikler

Daha fazla zamanım olsaydı ve kapsamı genişletmek isteseydim aşağıdakileri yapardım:

**Migration'ı çoklu kopyaya hazırlamak.** Migration servis açılırken çalışıyor. Birden fazla kopya aynı anda açılırsa çakışabilir. Goose'un kilit özelliğini açardım ya da migration'ı ayrı bir adım olarak çalıştırırdım.

**Org'lar için gerçek bir tablo.** Şu an her org ID geçerli ve org'un var olup olmadığı kontrol edilmiyor. Bir `orgs` tablosu ve `devices.org_id` üzerinde foreign key ekleyip olmayan org için 404 dönerdim. Org yönetimi kapsam dışı olduğu için yapmadım.

**Liste için sayfalama.** `GET /orgs/{orgID}/devices` tüm cihazları dönüyor. Org başına limit 5 olduğu için şimdilik sorun değil, ama limit büyütülürse sayfalama gerekir.

**Health check ve observability** Servisin `/healthz` gibi bir endpoint'i yok, bu yüzden compose'ta uygulama için healthcheck tanımlayamadım. İstek logu, metrik ve istek zaman aşımı da yok. HTTP sunucusunda sadece `ReadHeaderTimeout` var.

**Kapanışın otomatik testi.** Graceful shutdown'ı `docker compose stop` ile elle doğruladım ama bunun için otomatik bir test yazmadım. Worker ile heartbeat'in aynı anda çalıştığı durum için de deterministik bir test yok, bu davranışın doğruluğunu tek bir atomik SQL sorgusuna dayandırdım.

**Yanlış metot için 405.** Tanımlı bir yola yanlış metotla istek atılırsa 405 yerine 404 dönüyor, çünkü tanımsız yollar için tutarlı bir JSON hata gövdesi dönen genel bir route ekledim.

**Test veritabanı koruması.** Entegrasyon testleri `TEST_DATABASE_URL`'in gösterdiği veritabanındaki `devices` tablosunu temizliyor. Yanlışlıkla gerçek bir veritabanı gösterilirse veri silinir. Ayrı bir test veritabanı zorunlu kılardım.

## 6. AI kullanımı

Bu projede yapay zekâyı aşağıdaki noktalarda destek aracı olarak kullandım:

- **Paket araştırması:** Go paketlerine çok hâkim olmadığım için hangi paketleri kullanmam gerektiğini araştırırken yapay zekâdan destek aldım. Paketler arasındaki farkları ve normalde faydalı olsa da küçük bir proje için gereksiz kalabilecek olanları bu şekilde belirledim.
- **İş planı:** Projenin geliştirme adımlarını içeren iş planını yapay zekâ ile birlikte çıkardım.
- **Veritabanı bağlantısı:** Bu konuda eksiklerim olduğu için veritabanı bağlantısını yapay zekâ ile birlikte geliştirdim.
- **Error handling kontrolü:** Error handling kısmını kendim yazdım. Ardından yapay zekâ ile birlikte üzerinden geçip gözden kaçırdığım bir durum olup olmadığını kontrol ettim.
- **Code review:** Yazdığım kod üzerinde yapay zekâya bir code review turu yaptırdım ve clean code prensiplerine uygunluğunu kontrol ettim.
