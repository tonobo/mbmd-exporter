# syntax=docker/dockerfile:1
FROM ruby:3-alpine
RUN apk add --no-cache git libcurl ruby-dev build-base libffi-dev && mkdir -p /app
COPY . /app
WORKDIR /app
RUN bundle install
CMD bundle exec ruby app.rb -o 0.0.0.0

