# Jeu du Pendu en Go

Jeu du pendu jouable dans le navigateur, avec un serveur web écrit en Go et la bibliothèque standard uniquement (`net/http`, `html/template`).

Projet réalisé en Bachelor 1 Informatique à Ynov.

## Fonctionnalités

- Un mot tiré au hasard dans `words.txt`, avec une lettre révélée au départ
- 6 tentatives, avec un dessin du pendu qui avance à chaque erreur
- Les lettres avec accent sont gérées (le mot est traité en runes)
- Saisie vérifiée : une seule lettre, pas de lettre déjà proposée
- Une partie par joueur, grâce à un cookie de session : plusieurs personnes peuvent jouer en même temps

## Stack

- Go 1.22+, sans dépendance externe
- Templates HTML rendus côté serveur
- HTML / CSS

## Lancer en local

```bash
go run .
```

Puis ouvrir http://localhost:7580. Le port se change avec la variable `PORT`.

## Déploiement sur Render

Le fichier `render.yaml` décrit le service. Sur Render : **New > Blueprint**, choisir ce dépôt, puis valider. Render compile le binaire et le lance sur le port qu'il fournit.

## Structure

```
main.go          serveur, routes et logique du jeu
templates/       pages accueil, jeu et résultat
static/          CSS, images et dessins du pendu (SVG)
words.txt        liste des mots
render.yaml      configuration Render
```
