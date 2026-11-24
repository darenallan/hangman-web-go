package main

import (
	"bufio"             // Pour lire des fichiers ligne par ligne
	crand "crypto/rand" // Pour générer des identifiants de session imprévisibles
	"encoding/hex"      // Pour convertir l'identifiant de session en texte
	"fmt"               // Pour afficher des messages dans la console
	"html/template"     // Pour gérer les templates HTML
	"math/rand"         // Pour générer des nombres aléatoires
	"net/http"          // Pour créer le serveur web
	"os"                // Pour ouvrir et lire des fichiers
	"strings"           // Pour manipuler les chaînes de caractères
	"sync"              // Pour protéger les parties partagées entre requêtes
	"time"              // Pour nettoyer les parties abandonnées
	"unicode"           // Pour vérifier si un caractère est une lettre
)

// Game représente l'état d'une partie de pendu
type Game struct {
	Mot            string    // Le mot à deviner (en minuscules)
	Runes          []rune    // Le mot sous forme de tableau de caractères (supporte les accents)
	Saisi          []bool    // Tableau indiquant quelles lettres ont été trouvées
	Tentatives     int       // Nombre de tentatives restantes
	TentativeMax   int       // Nombre maximum de tentatives (ex: 6)
	LettresSaisies []rune    // Liste des lettres déjà proposées par le joueur
	Revelee        rune      // Lettre révélée automatiquement au début du jeu
	DerniereVisite time.Time // Dernière activité du joueur (pour le nettoyage)
}

// Nom du cookie qui identifie la partie de chaque joueur
const cookieSession = "pendu_session"

// Variables globales pour stocker l'état du jeu et les templates
var (
	mots         []string             // Liste de tous les mots possibles
	parties      = map[string]*Game{} // Une partie par joueur (clé = identifiant de session)
	mu           sync.Mutex           // Protège la map des parties
	tmplAccueil  *template.Template   // Template de la page d'accueil
	tmplJeu      *template.Template   // Template de la page de jeu
	tmplResultat *template.Template   // Template de la page de résultat
)

// Resultat contient le message à afficher sur la page de résultat
type Resultat struct {
	Message string // Message de victoire ou défaite
}

// PageJeuData contient toutes les données envoyées au template de jeu
type PageJeuData struct {
	MotAffiche       string // Le mot avec les lettres trouvées et des _ pour les autres
	Tentatives       int    // Nombre de tentatives restantes
	ImageHangman     int    // Numéro de l'image du pendu à afficher (0 à 6)
	LettreAleatoire  string // Lettre révélée au démarrage (informatif)
	LettresUtilisees string // Toutes les lettres déjà essayées
	Message          string // Message d'erreur éventuel
}

// main est le point d'entrée du programme
func main() {
	var err error
	// Charger la liste des mots depuis le fichier words.txt
	mots, err = ChargerMots("words.txt")
	// Vérifier si le chargement a réussi et si la liste n'est pas vide
	if err != nil || len(mots) == 0 {
		fmt.Println("Erreur lors du chargement des mots:", err)
		return // Arrêter le programme si erreur
	}

	// Précharger les templates HTML pour éviter de les relire à chaque requête
	tmplAccueil = template.Must(template.ParseFiles("templates/index.html"))
	tmplJeu = template.Must(template.ParseFiles("templates/index_jeu.html"))
	tmplResultat = template.Must(template.ParseFiles("templates/index_resultat.html"))

	// Configurer le serveur de fichiers statiques (CSS, images, etc.)
	fs := http.FileServer(http.Dir("static"))                 // Créer un serveur de fichiers pour le dossier "static"
	http.Handle("/static/", http.StripPrefix("/static/", fs)) // Associer l'URL /static/ au dossier static

	// Définir les routes (URLs) et leurs handlers (fonctions)
	http.HandleFunc("/", pageAccueil)          // Page d'accueil
	http.HandleFunc("/jeu", pageJeu)           // Page de jeu
	http.HandleFunc("/resultat", pageResultat) // Page de résultat

	// Le port est fourni par l'hébergeur (Render) via la variable PORT, 7580 en local
	port := os.Getenv("PORT")
	if port == "" {
		port = "7580"
	}
	fmt.Println("Serveur démarré sur http://localhost:" + port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Println("Erreur serveur:", err) // Afficher l'erreur si le serveur ne démarre pas
	}
}

// partieDuJoueur retourne la partie du joueur (identifié par un cookie), en la créant si besoin.
// L'appelant doit détenir le verrou mu.
func partieDuJoueur(w http.ResponseWriter, r *http.Request) *Game {
	if c, err := r.Cookie(cookieSession); err == nil {
		if g, ok := parties[c.Value]; ok {
			g.DerniereVisite = time.Now()
			return g
		}
	}

	// Nouveau joueur : on nettoie les parties inactives depuis plus d'une heure
	for id, g := range parties {
		if time.Since(g.DerniereVisite) > time.Hour {
			delete(parties, id)
		}
	}

	b := make([]byte, 16)
	crand.Read(b)
	id := hex.EncodeToString(b)
	g := &Game{DerniereVisite: time.Now()}
	parties[id] = g
	http.SetCookie(w, &http.Cookie{Name: cookieSession, Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	return g
}

// pageAccueil affiche la page d'accueil du jeu
func pageAccueil(w http.ResponseWriter, r *http.Request) {
	// Exécuter le template de la page d'accueil et l'envoyer au client
	if err := tmplAccueil.Execute(w, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError) // Erreur 500 si problème
	}
}

// pageJeu gère la logique principale du jeu et affiche la page de jeu
func pageJeu(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	game := partieDuJoueur(w, r)

	// Si aucune partie n'est en cours, en initialiser une nouvelle
	if game.Mot == "" {
		InitialiserJeu(game, mots)
	}

	// Traitement d'une lettre envoyée par le joueur (méthode POST)
	if r.Method == http.MethodPost {
		// Parser les données du formulaire
		if err := r.ParseForm(); err == nil {
			lettre := r.FormValue("lettre")         // Récupérer la lettre saisie
			rs := []rune(strings.TrimSpace(lettre)) // Convertir en runes et supprimer les espaces
			if len(rs) == 1 {                       // Vérifier qu'il y a exactement une lettre
				lr := unicode.ToLower(rs[0]) // Convertir en minuscule
				if !unicode.IsLetter(lr) {   // Vérifier que c'est bien une lettre
					http.Redirect(w, r, "/jeu?error=invalid", http.StatusSeeOther) // Rediriger avec erreur
					return
				}
				if LettreDejaSaisie(game, lr) { // Vérifier si la lettre a déjà été saisie
					http.Redirect(w, r, "/jeu?error=used", http.StatusSeeOther) // Rediriger avec erreur
					return
				}
				MettreAJourJeu(game, lr) // Mettre à jour le jeu avec la nouvelle lettre
			}
		}
	}

	// Vérifier si le joueur a gagné (toutes les lettres trouvées)
	if LettresTrouvees(game) {
		http.Redirect(w, r, "/resultat?status=win", http.StatusSeeOther) // Rediriger vers victoire
		return
	} else if game.Tentatives <= 0 { // Vérifier si le joueur a perdu (plus de tentatives)
		http.Redirect(w, r, "/resultat?status=lose", http.StatusSeeOther) // Rediriger vers défaite
		return
	}

	// Relancer une nouvelle partie si le bouton "Recommencer" est cliqué
	if r.Method == http.MethodGet {
		if r.URL.Query().Get("recommencer") == "true" {
			InitialiserJeu(game, mots) // Réinitialiser le jeu
		}
	}

	// Préparer le message d'erreur éventuel à afficher
	msg := ""
	switch r.URL.Query().Get("error") {
	case "used":
		msg = "Lettre déjà saisie — essayez une autre." // Message si lettre déjà utilisée
	case "invalid":
		msg = "Entrée invalide — veuillez entrer une lettre." // Message si entrée invalide
	}

	// Préparer les données à envoyer au template
	data := PageJeuData{
		MotAffiche:       AfficherMot(game),                    // Mot avec _ pour lettres non trouvées
		Tentatives:       game.Tentatives,                      // Tentatives restantes
		ImageHangman:     game.TentativeMax - game.Tentatives,  // Index de l'image du pendu
		LettreAleatoire:  string(ChoisirLettreAleatoire(game)), // Lettre révélée (informatif)
		LettresUtilisees: FormatLettresSaisies(game),           // Historique des lettres
		Message:          msg,                                  // Message d'erreur éventuel
	}

	// Afficher la page de jeu avec les données
	if err := tmplJeu.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError) // Erreur 500 si problème
	}
}

// pageResultat affiche la page de résultat (victoire ou défaite)
func pageResultat(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	game := partieDuJoueur(w, r)

	status := r.URL.Query().Get("status") // Récupérer le statut (win ou lose)
	message := "Résultat du jeu"          // Message par défaut

	// Définir le message selon le résultat
	switch status {
	case "win":
		message = "Félicitations ! Vous avez gagné ! 🥳" // Message de victoire
	case "lose":
		message = "Désolé, vous avez perdu. 😭 Le mot était : " + game.Mot // Message de défaite
	}

	// Afficher la page de résultat
	if err := tmplResultat.Execute(w, Resultat{Message: message}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Réinitialiser le jeu pour permettre une nouvelle partie
	*game = Game{DerniereVisite: time.Now()}
}

// ChargerMots lit un fichier texte et retourne une liste de mots
func ChargerMots(filename string) ([]string, error) {
	f, err := os.Open(filename) // Ouvrir le fichier
	if err != nil {
		return nil, err // Retourner l'erreur si le fichier n'existe pas
	}
	defer f.Close() // Fermer le fichier à la fin de la fonction

	var out []string          // Liste pour stocker les mots
	sc := bufio.NewScanner(f) // Créer un scanner pour lire ligne par ligne
	for sc.Scan() {           // Lire chaque ligne
		s := strings.TrimSpace(sc.Text()) // Supprimer les espaces en début/fin
		if s != "" {                      // Ignorer les lignes vides
			out = append(out, s) // Ajouter le mot à la liste
		}
	}
	if err := sc.Err(); err != nil { // Vérifier s'il y a eu une erreur de lecture
		return nil, err
	}
	return out, nil // Retourner la liste des mots
}

// InitialiserJeu crée une nouvelle partie avec un mot aléatoire
func InitialiserJeu(game *Game, mots []string) {
	mot := mots[rand.Intn(len(mots))]          // Choisir un mot au hasard dans la liste (générateur auto-initialisé depuis Go 1.20)
	game.Mot = strings.ToLower(mot)            // Stocker le mot en minuscules
	game.Runes = []rune(game.Mot)              // Convertir le mot en tableau de caractères
	game.Saisi = make([]bool, len(game.Runes)) // Créer un tableau de booléens (toutes les lettres cachées)
	game.TentativeMax = 6                      // Définir le nombre max de tentatives
	game.Tentatives = game.TentativeMax        // Initialiser les tentatives restantes
	game.LettresSaisies = nil                  // Vider l'historique des lettres (implicite avec réinitialisation)

	// Révéler une lettre au hasard au démarrage pour aider le joueur
	game.Revelee = ChoisirLettreAleatoire(game)
	for i, r := range game.Runes { // Parcourir chaque lettre du mot
		if r == game.Revelee { // Si c'est la lettre révélée
			game.Saisi[i] = true // Marquer comme trouvée
		}
	}
}

// AfficherMot retourne le mot avec les lettres trouvées et des _ pour les autres
func AfficherMot(g *Game) string {
	var b strings.Builder       // Builder pour construire la chaîne efficacement
	for i, r := range g.Runes { // Parcourir chaque lettre du mot
		if g.Saisi[i] { // Si la lettre a été trouvée
			b.WriteRune(r) // Afficher la lettre
		} else {
			b.WriteString("_") // Sinon afficher un underscore
		}
		b.WriteString(" ") // Ajouter un espace entre chaque caractère
	}
	return b.String() // Retourner la chaîne construite
}

// ChoisirLettreAleatoire retourne une lettre aléatoire du mot
func ChoisirLettreAleatoire(g *Game) rune {
	if len(g.Runes) == 0 { // Si le mot est vide
		return '_' // Retourner un underscore par défaut
	}
	return g.Runes[rand.Intn(len(g.Runes))] // Retourner une lettre au hasard
}

// FormatLettresSaisies retourne les lettres déjà saisies sous forme de chaîne
func FormatLettresSaisies(g *Game) string {
	if len(g.LettresSaisies) == 0 { // Si aucune lettre n'a été saisie
		return "" // Retourner une chaîne vide
	}
	var b strings.Builder                // Builder pour construire la chaîne
	for i, r := range g.LettresSaisies { // Parcourir chaque lettre saisie
		if i > 0 {
			b.WriteString(" ") // Ajouter un espace entre les lettres
		}
		b.WriteRune(r) // Ajouter la lettre
	}
	return b.String() // Retourner la chaîne construite
}

// LettreDejaSaisie vérifie si une lettre a déjà été proposée
func LettreDejaSaisie(g *Game, lettre rune) bool {
	for _, l := range g.LettresSaisies { // Parcourir l'historique des lettres
		if l == lettre { // Si la lettre est trouvée
			return true // Elle a déjà été saisie
		}
	}
	return false // La lettre n'a pas encore été saisie
}

// MettreAJourJeu traite une nouvelle lettre proposée par le joueur
func MettreAJourJeu(g *Game, lettre rune) {
	g.LettresSaisies = append(g.LettresSaisies, lettre) // Ajouter la lettre à l'historique
	found := false                                      // Variable pour savoir si la lettre est dans le mot
	for i, r := range g.Runes {                         // Parcourir chaque lettre du mot
		if r == lettre { // Si la lettre correspond
			g.Saisi[i] = true // Marquer comme trouvée
			found = true      // Indiquer qu'on a trouvé la lettre
		}
	}
	if !found { // Si la lettre n'est pas dans le mot
		g.Tentatives-- // Retirer une tentative
	}
}

// LettresTrouvees vérifie si toutes les lettres du mot ont été découvertes
func LettresTrouvees(g *Game) bool {
	for _, v := range g.Saisi { // Parcourir le tableau des lettres trouvées
		if !v { // Si une lettre n'est pas encore trouvée
			return false // Le mot n'est pas encore complet
		}
	}
	return true // Toutes les lettres ont été trouvées
}
