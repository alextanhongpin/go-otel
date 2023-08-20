include .env
export


run:
	@go run main.go


up:
	@docker-compose up -d


down:
	@docker-compose down


dashboard:
	open http://localhost:16686
	open http://localhost:9090
