package proxy

import (
	"fmt"
	"sync"
	"time"

	"github.com/sandertv/gophertunnel/minecraft"
)

func prepareGame(client *minecraft.Conn, server *minecraft.Conn) error {
	var w sync.WaitGroup
	errs := make(chan error, 2)

	w.Add(1)
	go func() {
		defer w.Done()
		errs <- client.StartGame(server.GameData())
	}()

	w.Add(1)
	go func() {
		defer w.Done()
		errs <- server.DoSpawn()
	}()

	w.Wait()

	for i := 0; i < 2; i++ {
		err := <-errs
		if err != nil {
			return err
		}
	}

	return nil
}

func (self *Context) handleClient(client *minecraft.Conn) error {
	defer client.Close()

	clientData := client.ClientData()

	self.logger.Infof("Accepted client: %v", clientData.ThirdPartyName)
	self.logger.Debug("Connecting to server")

	server, err := self.ConnectServer(false, clientData, time.Minute)

	if err != nil {
		return fmt.Errorf("failed to connect to server: %v", err)
	}

	defer server.Close()

	self.logger.Info("Connected!")
	self.logger.Info("Preparing game")

	if err := prepareGame(client, server); err != nil {
		return fmt.Errorf("failed to prepare game: %v", err)
	}

	for _, inj := range self.EnabledInjectors {
		self.logger.Debugf("Executing %s (%s) OnLogin", inj.Name(), inj.Version())
		inj.OnLogin(client, server)
	}

	self.logger.Info("Preparation done. Proxing packets!")
	var w sync.WaitGroup

	w.Add(1)
	go func() {
		defer w.Done()
		for {
			serverPacket, err := server.ReadPacket()

			if err != nil {
				self.logger.Errorf("Error reading packet: %v", err)

				break
			}

			for _, inj := range self.EnabledInjectors {
				serverPacket, err = inj.OnServerPacket(serverPacket)
				if err != nil {
					self.logger.Errorf("Error on plugin %s (OnServerPacket): %v", inj.Name(), err)
				}
			}

			err = client.WritePacket(serverPacket)

			if err != nil {
				self.logger.Errorf("Error writing packet: %v", err)

				break
			}
		}
	}()

	w.Add(1)
	go func() {
		defer w.Done()
		for {
			clientPacket, err := client.ReadPacket()

			if err != nil {
				self.logger.Errorf("Error reading packet: %v", err)

				break
			}

			for _, inj := range self.EnabledInjectors {
				clientPacket, err = inj.OnClientPacket(clientPacket)
				if err != nil {
					self.logger.Errorf("Error on plugin %s (OnClientPacket): %v", inj.Name(), err)
				}
			}

			err = server.WritePacket(clientPacket)

			if err != nil {
				self.logger.Errorf("Error writing packet: %v", err)

				break
			}
		}
	}()

	w.Wait()

	return nil
}
