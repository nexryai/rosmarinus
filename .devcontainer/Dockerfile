FROM ubuntu:resolute

RUN apt-get update && apt-get upgrade -y
RUN apt-get install -y git curl golang-go python3 libatomic1

RUN curl -fsSL https://deb.nodesource.com/setup_24.x | bash -
RUN apt-get install -y nodejs

RUN npm install -g pnpm @openai/codex
