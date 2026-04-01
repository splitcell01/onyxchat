package com.cole.securechat.ui;

import javafx.fxml.FXML;
import javafx.scene.control.ListView;

public class ChatListController {

    @FXML
    private ListView<String> contactsList;

    @FXML
    private void initialize() {
        // Use the actual usernames that exist in your DB
        contactsList.getItems().setAll(
                "alice",
                "bob",
                "charlie"
        );
        // Optionally pre-select the first contact
        if (!contactsList.getItems().isEmpty()) {
            contactsList.getSelectionModel().select(0);
        }
    }

    public String getSelectedContact() {
        return contactsList.getSelectionModel().getSelectedItem();
    }
}
