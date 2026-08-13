FROM python:3.12-slim

WORKDIR /app
COPY pyproject.toml README.md ./
COPY src ./src
COPY schemas ./schemas
COPY assets ./assets
RUN python -m pip install --no-cache-dir .

EXPOSE 4173
ENTRYPOINT ["safelane"]
CMD ["studio", "--repository", "AndrewMaged814/safelane", "--port", "4173"]
