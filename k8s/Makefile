.PHONY: secret deploy delete status scale port-forward

# Create or update the secret from secret.yaml (copy secret.example.yaml first).
secret:
	kubectl apply -f secret.yaml

# Apply everything. Run `make secret` first.
deploy:
	kubectl apply -f redis.yaml -f mysql.yaml
	kubectl rollout status deploy/redis deploy/mysql --timeout=180s
	kubectl apply -f app-deployment.yaml -f app-service.yaml -f nginx-configmap.yaml -f nginx-deployment.yaml -f nginx-service.yaml
	kubectl rollout status deploy/app deploy/nginx --timeout=180s

# Remove workloads. PVCs and the secret are kept; delete them by hand to wipe data.
delete:
	kubectl delete -f nginx-service.yaml -f nginx-deployment.yaml -f nginx-configmap.yaml -f app-service.yaml -f app-deployment.yaml --ignore-not-found
	kubectl delete deploy/mysql deploy/redis svc/mysql svc/redis --ignore-not-found

status:
	kubectl get deploy,pods,svc,pvc

# make scale N=10 (keep N x DB_MAX_OPEN_CONNS below MySQL --max-connections)
scale:
	kubectl scale deploy/app --replicas=$(N)

port-forward:
	kubectl port-forward svc/nginx 8080:80
