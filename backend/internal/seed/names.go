package seed

var firstNames = []string{
	"Ahmet", "Mehmet", "Ayşe", "Fatma", "Mustafa", "Emine", "Ali", "Hatice", "Hüseyin", "Zeynep",
	"Hasan", "Elif", "İbrahim", "Meryem", "Murat", "Şule", "Ömer", "Özlem", "Emre", "Gül",
	"Can", "Ece", "Burak", "Selin", "Cem", "Derya", "Kemal", "Esra", "Serkan", "Büşra",
	"Deniz", "Merve", "Onur", "Gizem", "Tolga", "İrem", "Volkan", "Pınar", "Barış", "Ceren",
	"Yusuf", "Damla", "Kaan", "Tuğba", "Sinan", "Aslı", "Oğuz", "Sevgi", "Uğur", "Çağla",
}

var lastNames = []string{
	"Yılmaz", "Kaya", "Demir", "Şahin", "Çelik", "Yıldız", "Yıldırım", "Öztürk", "Aydın", "Özdemir",
	"Arslan", "Doğan", "Kılıç", "Aslan", "Çetin", "Kara", "Koç", "Kurt", "Özkan", "Şimşek",
	"Polat", "Korkmaz", "Erdoğan", "Güneş", "Aksoy", "Tekin", "Ünal", "Bulut", "Keskin", "Acar",
}

var cities = []string{"Balıkesir", "Bandırma", "Bursa", "İstanbul", "İzmir", "Çanakkale", "Ankara", "Erdek", "Gönen", "Susurluk"}

var dogNames = []string{"Karabaş", "Boncuk", "Max", "Çomar", "Paşa", "Zeytin", "Fındık", "Lucky", "Rocky", "Bella", "Duman", "Tarçın", "Badem", "Cesur", "Şila", "Toprak", "Kont", "Lessie", "Mia", "Coco"}
var catNames = []string{"Pamuk", "Tekir", "Minnoş", "Duman", "Mırnav", "Boncuk", "Şeker", "Limon", "Mia", "Zeytin", "Fıstık", "Lokum", "Kömür", "Sarman", "Bulut", "Luna", "Leo", "Simba", "Nala", "Garfield"}
var rabbitNames = []string{"Pofuduk", "Havuç", "Kartopu", "Ponpon", "Zıpzıp", "Fındık", "Bulut", "Tavşi"}

type breedInfo struct {
	Name      string
	MinKg     float64
	MaxKg     float64
	Weight    int
	Collieish bool
	HighParvo bool
}

var dogBreeds = []breedInfo{
	{Name: "mixed", MinKg: 8, MaxKg: 30, Weight: 30},
	{Name: "Golden Retriever", MinKg: 25, MaxKg: 36, Weight: 8},
	{Name: "Labrador", MinKg: 25, MaxKg: 38, Weight: 8},
	{Name: "Kangal", MinKg: 40, MaxKg: 65, Weight: 6},
	{Name: "Alman Kurdu", MinKg: 22, MaxKg: 40, Weight: 7},
	{Name: "Rottweiler", MinKg: 35, MaxKg: 60, Weight: 4, HighParvo: true},
	{Name: "Doberman", MinKg: 30, MaxKg: 45, Weight: 3, HighParvo: true},
	{Name: "Collie", MinKg: 18, MaxKg: 30, Weight: 3, Collieish: true},
	{Name: "Shetland Sheepdog", MinKg: 6, MaxKg: 12, Weight: 2, Collieish: true},
	{Name: "Pomeranian", MinKg: 1.8, MaxKg: 3.5, Weight: 5},
	{Name: "Chihuahua", MinKg: 1.5, MaxKg: 3, Weight: 4},
	{Name: "Beagle", MinKg: 9, MaxKg: 14, Weight: 5},
	{Name: "Terrier", MinKg: 5, MaxKg: 10, Weight: 5},
}

var catBreeds = []breedInfo{
	{Name: "mixed", MinKg: 3, MaxKg: 6, Weight: 50},
	{Name: "Tekir", MinKg: 3, MaxKg: 6, Weight: 15},
	{Name: "British Shorthair", MinKg: 4, MaxKg: 8, Weight: 10},
	{Name: "Scottish Fold", MinKg: 3, MaxKg: 6, Weight: 8},
	{Name: "Van Kedisi", MinKg: 3.5, MaxKg: 7, Weight: 6},
	{Name: "Ankara Kedisi", MinKg: 2.5, MaxKg: 5, Weight: 6},
	{Name: "Persian", MinKg: 3, MaxKg: 6, Weight: 5},
}

var rabbitBreeds = []breedInfo{
	{Name: "mixed", MinKg: 1.2, MaxKg: 3, Weight: 5},
	{Name: "Hollanda Lop", MinKg: 1.2, MaxKg: 2, Weight: 3},
	{Name: "Aslan Baş", MinKg: 1.1, MaxKg: 1.7, Weight: 2},
}

var findings = []string{
	"Genel durum iyi, mukozalar pembe, lenf nodları normal.",
	"Kulakta hafif kızarıklık ve akıntı, otitis eksterna şüphesi.",
	"Deride kaşıntı ve tüy dökülmesi, parazit kontrolü yapıldı.",
	"İştahsızlık ve kusma şikâyeti, abdomen palpasyonunda hassasiyet.",
	"Arka bacakta topallık, ortopedik muayene yapıldı.",
	"Diş taşı ve diş eti iltihabı gözlendi.",
	"Rutin kontrol, kilo takibi yapıldı.",
	"Gözde akıntı ve kızarıklık, konjonktivit.",
	"Hafif dehidrasyon, sıvı tedavisi önerildi.",
	"Aşı öncesi genel muayene, aşıya engel durum yok.",
}

var diagnoses = []string{"Sağlıklı", "Otitis eksterna", "Dermatit", "Gastroenterit", "Topallık", "Periodontal hastalık", "Konjonktivit", "Dehidrasyon"}

var emergencyFindings = []string{
	"ACİL: Trafik kazası, travma ve şok bulguları.",
	"ACİL: Zehirlenme şüphesi, hipersalivasyon ve kusma.",
	"ACİL: Mide dönmesi (GDV) şüphesi, şişkin abdomen.",
	"ACİL: Solunum güçlüğü, siyanoz.",
	"ACİL: Doğum güçlüğü (distosi).",
}
