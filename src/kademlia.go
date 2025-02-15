package src

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net"
)

// Kademlia nodes store contact information about each other <IP, UDP port, Node ID>
type Kademlia struct {
	NetworkInterface NetworkInterface
	node_contact     RoutingTable
	data             map[string][]byte
}

func InitNode(address *net.UDPAddr) Kademlia {
	//address, _ := GetLocalAddr()
	var id_node *KademliaID = NewRandomKademliaID()
	var new_contact Contact = NewContact(id_node, address.String())
	var routing_table RoutingTable = *NewRoutingTable(new_contact)
	//var new_network Network = InitNetwork()

	fmt.Printf("New node was created with: \n Address: %s\n Contact: %s\n ID: %s\n", address.String(), new_contact.String(), id_node.String())
	kademlia := Kademlia{
		//NetworkInterface: network,
		node_contact: routing_table,
		data:         make(map[string][]byte),
	}
	return kademlia
}

func (kademlia *Kademlia) SetNetworkInterface(network NetworkInterface) {
	kademlia.NetworkInterface = network
}

func (kademlia *Kademlia) ShowNodeBucketStatus() {
	//current_buckets := network.node.node_contact.buckets
	//known_buckets := network.node.node_contact.buckets
	known_buckets := kademlia.node_contact.buckets
	for i := 0; i < len(known_buckets); i++ {
		bucket := known_buckets[i]
		if bucket.Len() > 0 {
			bucket.ShowContactsInBucket()
		}
	}
}

// This function update appropriate k-bucket for the sender's node ID.
// The argument takes the target contacts bucket received from requests or response
func (kademlia *Kademlia) UpdateHandleBuckets(target_contact Contact) {

	// Fetch the correct bucket location based on bucket index
	bucket_index := kademlia.node_contact.getBucketIndex(target_contact.ID)
	target_bucket := kademlia.node_contact.buckets[bucket_index]

	if !target_contact.ID.Equals(kademlia.node_contact.me.ID) {

		// if bucket is not full = add the node to the bucket
		if target_bucket.Len() < GetMaximumBucketSize() && target_bucket.DoesBucketContactExist(target_contact) {
			fmt.Printf("Bucket contact already exist adding the contact to tail: %s\n", target_contact.Address)
			kademlia.node_contact.AddContact(target_contact)

		} else if target_bucket.Len() == GetMaximumBucketSize() {
			// If bucket is full - ping the k-bucket's least-recently seen node
			// least-recently node at the head & most-recently node at the tail

			least_recently_node := target_bucket.GetLeastRecentlyNode() // contact at the tail
			least_recently_addr, _ := net.ResolveUDPAddr("udp", least_recently_node.Address)

			my_contact := kademlia.node_contact.me
			fmt.Printf("Bucket was full trying to ping recently-seen node: %s\n", least_recently_node.Address)
			rpc_ping, request_err := kademlia.NetworkInterface.FetchRPCResponse(Ping, "bucket_full_ping_id", &my_contact, least_recently_addr)

			// Might not need this rpc_ping handling since every rpc will still run the 'UpdateHandleBuckets method'

			// Todo: Maybe add a new branch for checking the ping for the k-bucket's least_recently_node

			if request_err != nil || rpc_ping.Error != nil {
				//failed to response - removed from the k-bucket and new sender inserted at the tail
				fmt.Println("Request was unsuccessful, removing least-recently seen node from bucket: ", least_recently_node.Address)
				kademlia.node_contact.RemoveTargetContact(*least_recently_node)

			} else if rpc_ping.Error == nil {
				//successful response -  contact is moved to the tail of the list
				fmt.Printf("Ping was successful adding/moving contact: %s to tail\n", rpc_ping.Contact.Address)
				kademlia.node_contact.AddContact(rpc_ping.Contact)
			}

		} else {
			kademlia.node_contact.AddContact(target_contact)
		}

	}
	kademlia.ShowNodeBucketStatus()
}

func (kademlia *Kademlia) AsynchronousLookupContact(target_contact *Contact) []Contact {
	alpha := 3
	contacted_nodes := ContactCandidates{}
	result_shortlist := ContactCandidates{kademlia.node_contact.FindClosestContacts(target_contact.ID, alpha)}
	response_channel := make(chan PayloadData, alpha) // Create a temporary response channel of size alpha for contact and contacts handling

	for len(contacted_nodes.contacts) < alpha && result_shortlist.Len() > 0 {

		for i := 0; i < result_shortlist.Len() && len(contacted_nodes.contacts) < alpha; i++ {

			contact := result_shortlist.contacts[i] //known contact
			target_addr, _ := net.ResolveUDPAddr("udp", contact.Address)

			// Check if node has already been contacted then continue else AsynchronousFindNode
			contact_exist := contactExists(&contact, contacted_nodes)

			if contact_exist {
				fmt.Println("The node has already (skipping FIND_NODE) been contacted: ", contact)
				continue
			} else {
				go kademlia.NetworkInterface.AsynchronousFindNode(target_contact, target_addr, response_channel)
			}

		}
		// TO FIX:
		// 1. Make so it updates already contacted
		// 2. Update shortlist

		// Iterate through the responses and update the shortlist
		for i := 0; i < result_shortlist.Len() && len(contacted_nodes.contacts) < alpha; i++ {
			k_response := <-response_channel
			k_closest := k_response.Contacts
			k_contact := k_response.Contact
			fmt.Println("Asynchronous k-response: ", k_response)

			if k_response.Error == nil && len(k_closest) > 0 {
				contacted_nodes.contacts = append(contacted_nodes.contacts, k_contact)
				// Update the shortlist based on the received response.
				updatedShortlist := updateShortlist(result_shortlist, k_closest, target_contact.ID)
				result_shortlist = updatedShortlist
				fmt.Println("Appended to contacted nodes list, now contacted nodes: ", contacted_nodes.contacts)
			} else {
				fmt.Println("FIND_NODE RPC failed response, removing the node contact now!")
				kademlia.node_contact.RemoveTargetContact(k_response.Contact)
			}
		}
		if result_shortlist.Len() >= alpha {
			return result_shortlist.contacts
		}

	}
	return result_shortlist.contacts
}

func updateShortlist(shortlist ContactCandidates, newNodes []Contact, targetID *KademliaID) ContactCandidates {
	updatedShortlist := shortlist

	for _, newNode := range newNodes {
		// Iterate over the new nodes from the response.
		// Check if any of them are closer to the target.
		for i, existingNode := range updatedShortlist.contacts {
			if newNode.ID.Less(existingNode.ID) {
				updatedShortlist.contacts[i] = newNode // Replace the existing node with the closer one.
				break
			}
		}
	}

	// Sort the updated shortlist by distance to the target.
	updatedShortlist.Sort()
	return updatedShortlist
}

func contactExists(contact *Contact, contactedNodes ContactCandidates) bool {
	for _, existingContact := range contactedNodes.contacts {
		if existingContact.ID.Equals(contact.ID) {
			return true
		}
	}
	return false
}

func (kademlia *Kademlia) refresh() {

}

func (kademlia *Kademlia) LookupData(hash string) ([]byte, bool) {
	// Take the sha1 encryption and check if it exists as a key
	original, exists := kademlia.data[hash] // On self
	fmt.Println("")
	var a []Contact

	if exists {
		fmt.Printf("The data you want already exists: %s \n", original)
	} else {
		fmt.Println("The data can't be found on self, searching through K closest nodes")
		//boot_addr, _ := GetBootnodeAddr()
		//contact_me := kademlia.node_contact.me
		//bootnode_contact, _ := kademlia.NetworkInterface.FetchRPCResponse(Ping, "boot_node_contact_id", &contact_me, boot_addr)
		a = kademlia.node_contact.FindClosestContacts(kademlia.node_contact.me.ID, 3) // Find my 3 closest nodes
		for i := 0; i < len(a); i++ {
			target_node := a[i]
			target_addr, _ := net.ResolveUDPAddr("udp", target_node.Address)
			value_response, _ := kademlia.NetworkInterface.FetchRPCResponse(FindValue, "", &kademlia.node_contact.me, target_addr)

			kademlia.Store((value_response.Value))
			fmt.Printf("Object found and downloaded: %s", string(value_response.Value))
		}
	}
	return original, exists
}

func (kademlia *Kademlia) Store(data []byte) {
	// Encrypt the hash for our value
	hash := kademlia.Hash(data)

	// Save the key value pair to current node
	kademlia.data[hash] = data
	//fmt.Println("Storing key value pair, DONE")
	fmt.Println("Stored the hash: " + hash)
	//fmt.Println("Key value is: " + kademlia.data[hash])
	return
}

func (kademlia *Kademlia) Hash(data []byte) string {
	// Create the hash value
	hasher := sha1.Sum(data)

	// Convert the hash to hexadecmial string
	hash := hex.EncodeToString(hasher[0:IDLength])
	fmt.Println("Hashing DONE")
	return hash
}
