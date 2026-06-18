# TODO

## CI / qualité

- [ ] Créer un workflow `.github/workflows/ci.yml` minimal (`go test -tags=testing ./...`, `biome check`, build frontend) pour ne plus dépendre de lefthook côté contributeur.
- [ ] Mettre en place des tests frontend (vitest) — au minimum sur la logique du dashboard (filtres, agrégation de sévérité, tri).

## Dette technique

- [ ] Supprimer `SSHTransport` et le code mort associé dans `internal/hub/transport/`.
- [ ] Statuer sur la stratégie de maintenance documentaire : accepter une dérive contrôlée OU automatiser (lint des liens internes, check de couverture doc/code). La section "Documentation Maintenance" d'`AGENTS.md` n'est pas tenable sur 12 mois en solo.

## Produit / positionnement

- [ ] Vérifier la cohérence du `README.md` avec l'état réel du projet (features, env vars, structure).
- [ ] Rendre explicite le principe "agents en lecture seule par design" dans le `README.md` — proposition de valeur clé vs Beszel/PatchMon/Portainer.
- [ ] Graver cette garantie au niveau du protocole plutôt qu'en convention : commentaire en tête de `internal/common/common-ws.go` ("ajouter une action d'écriture/exécution = changement de modèle de sécurité") et/ou test qui vérifie qu'aucun handler agent ne touche au système.
- [ ] Vérifier que les notifications d'images conteneur couvrent les usages Watchtower / Diun en mode "alerte uniquement" : nouvelle version dispo, digest changé, image retirée du registry. Pas d'auto-update.
