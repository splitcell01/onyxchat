package com.cole.securechat;

import javafx.application.Application;
import javafx.fxml.FXMLLoader;
import javafx.scene.Parent;
import javafx.scene.Scene;
import javafx.stage.Stage;

public class Main extends Application {

    @Override
    public void start(Stage stage) throws Exception {
        // Load the first screen (login)
        FXMLLoader loader = new FXMLLoader(Main.class.getResource("/login.fxml"));
        Parent root = loader.load();

        // Create scene with a modern default size
        Scene scene = new Scene(root, 1080, 720);

        // Attach global Onyx Dark stylesheet
        String onyxCss = Main.class
                .getResource("/onyx-dark.css")
                .toExternalForm();
        scene.getStylesheets().add(onyxCss);

        // Stage config
        stage.setTitle("Onyx Chat");
        stage.setMinWidth(960);
        stage.setMinHeight(600);
        stage.setScene(scene);
        stage.show();
    }

    public static void main(String[] args) {
        launch(args);
    }
}
