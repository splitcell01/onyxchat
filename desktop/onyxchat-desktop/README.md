# Secure Messenger — JavaFX Desktop Client

A lightweight desktop application for a secure messaging system. Built using **Java 17**, **JavaFX**, and **Maven**, this client communicates with the Go backend server over a REST API and will eventually support full end‑to‑end encrypted messaging.

---

## 🚀 Overview

The desktop client provides:

* JavaFX UI for **login** and **registration**
* REST communication with backend API
* Clean MVC structure with FXML views
* Extensible foundation for:

    * Encrypted chats
    * Live messaging
    * User presence and status
    * Attachments and notifications

This repo is meant to serve as the **frontend** portion of the Secure Messenger project.

> 🔐 Security Note  
> This project is a work in progress. Transport security and E2E encryption are under active development and **should not be considered production-grade yet**.


---

## ✨ Current Features

- JavaFX login + registration screens
- REST calls to backend `/register` (and `/login` if wired)
- Basic form validation (username/password required)
- Configurable backend URL via `AppConfig`

---

## 🧠 Tech Highlights

- MVC-style JavaFX desktop architecture (FXML views + controller classes)
- Decoupled HTTP client layer using `HttpClient` and JSON (Gson/Jackson)
- Configurable backend URL, ready for local or remote deployment
- Designed to evolve toward E2E-encrypted, real-time messaging

---

## 🗂 Project Structure

```
secure-messenger-desktop/
  src/main/java/
    com/securemessenger/
      Main.java
      http/                 # HTTP client wrappers
      controllers/          # JavaFX controllers
      config/               # App configuration
  src/main/resources/
    fxml/                   # JavaFX UI layout files
    css/                    # Stylesheets
  pom.xml
```

*(Adjust the package names above to match your project if needed.)*

---

## 📦 Requirements

* Java **17+**
* Maven **3.8+**
* JavaFX dependencies (handled via Maven)
* Secure Messenger Go backend running locally or remotely

Default backend URL:

```
http://localhost:8080
```

---

## ▶️ Running the Application

### 1. Clone the repository

```
git clone https://github.com/cole/secure-messenger-desktop.git
cd secure-messenger-desktop
```

### 2. Ensure backend server is running

From your server project:

```
go run ./cmd/server
```

### 3. Build the desktop client

```
mvn clean install
```

### 4. Launch the app

```
mvn javafx:run
```

A JavaFX login window should appear.

---

## 🔌 Backend Integration

The client communicates with the backend via JSON over HTTP.

### Register Endpoint

**POST** `/api/v1/register`

```json
{
  "username": "cole",
  "password": "hunter2"
}
```

The desktop app uses Java's `HttpClient` for requests and a JSON library (Gson/Jackson) for serialization.

Backend repo:

```
https://github.com/cole/secure-messenger-server
```

---

## ⚙️ Configuration

Example config class:

```java
public class AppConfig {
    public static final String BASE_URL = "http://localhost:8080";
}
```

You can later move this to:

* External config file
* Environment variables
* A settings screen

---

## 🔮 Roadmap

* [ ] Login endpoint integration
* [ ] Chat window UI
* [ ] Message send/receive functionality
* [ ] Local encrypted key storage
* [ ] E2E encrypted messaging
* [ ] User profiles & settings
* [ ] Packaging into native installers using jpackage

---

## 📝 License

MIT License (or any license you prefer).

---

## 🤝 Related Project

This client works with the Go backend:

```
https://github.com/cole/secure-messenger-server
```
